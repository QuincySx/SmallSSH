package snippet

import (
	"strings"
	"testing"
)

func sample() Command {
	return Command{
		ID: "update", Name: "更新系统", Group: "维护",
		Body: "sudo apt update && sudo apt install -y $1",
		Variants: []Variant{
			{OS: OSRedHat, Body: "sudo dnf upgrade -y $1"},
			{OS: OSMacOS, Body: "brew upgrade $1"},
		},
		Args: []Arg{{Type: TypeText, Preset: "nginx"}},
	}
}

func TestRenderPicksTheBodyForTheSystemAndFillsTypedPlaceholders(t *testing.T) {
	command := sample()
	for _, test := range []struct {
		system string
		values []string
		want   string
	}{
		{OSDebian, []string{"curl"}, "sudo apt update && sudo apt install -y curl"},
		{OSArch, []string{"curl"}, "sudo apt update && sudo apt install -y curl"},
		{OSRedHat, nil, "sudo dnf upgrade -y "},
		{OSMacOS, []string{"git"}, "brew upgrade git"},
	} {
		got, err := Render(command, test.system, test.values)
		if err != nil || got != test.want {
			t.Fatalf("%s: got %q err %v, want %q", test.system, got, err, test.want)
		}
	}
	withoutDefault := Command{ID: "x", Name: "x", Variants: []Variant{{OS: OSRedHat, Body: "dnf upgrade"}}}
	if _, err := Render(withoutDefault, OSDebian, nil); err == nil || !strings.Contains(err.Error(), "没有可用") {
		t.Fatal("a system without any body was rendered", err)
	}
}

func TestPlaceholderSyntaxLeavesShellExpansionsAlone(t *testing.T) {
	command := Command{ID: "x", Name: "x", Body: "echo $HOME ${PATH} $$ $0 $1 ${2} $01", Args: []Arg{{}, {}}}
	got, err := Render(command, OSDefault, []string{"one", "two"})
	if err != nil {
		t.Fatal(err)
	}
	want := "echo $HOME ${PATH} $$ $0 one two $01"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if n := PlaceholderCount(command); n != 2 {
		t.Fatal("placeholder count", n)
	}
}

func TestNumericValuesAreValidatedWithoutQuotingText(t *testing.T) {
	command := Command{ID: "x", Name: "x", Body: "kill -9 $1 $2", Args: []Arg{{Type: TypeInt}, {Type: TypeNumber}}}
	if got, err := Render(command, OSDefault, []string{" 42 ", "1.5"}); err != nil || got != "kill -9 42 1.5" {
		t.Fatal(got, err)
	}
	for _, values := range [][]string{{"pid", "2"}, {"1", "many"}} {
		if _, err := Render(command, OSDefault, values); err == nil {
			t.Fatal("invalid numeric value accepted", values)
		}
	}
	text := Command{ID: "x", Name: "x", Body: "echo $1", Args: []Arg{{Type: TypeText}}}
	if got, err := Render(text, OSDefault, []string{"a b; rm -rf ~"}); err != nil || got != "echo a b; rm -rf ~" {
		t.Fatal("text was altered", got, err)
	}
}

func TestParameterDefaultsAndPromptChoice(t *testing.T) {
	command := Command{ID: "restart", Name: "restart", Body: "systemctl restart $1",
		Args: []Arg{{Name: "服务名", Preset: "nginx", SkipPrompt: true}}}
	if err := Validate(command); err != nil {
		t.Fatal(err)
	}
	if got := PresetValues(command, OSDefault); len(got) != 1 || got[0] != "nginx" {
		t.Fatal("defaults were not available for a non-asked parameter", got)
	}
	args := ArgsFor(command, OSDefault)
	if len(args) != 1 || args[0].Name != "服务名" || !args[0].SkipPrompt {
		t.Fatal("parameter metadata was lost", args)
	}
	if got, err := Render(command, OSDefault, PresetValues(command, OSDefault)); err != nil || got != "systemctl restart nginx" {
		t.Fatal(got, err)
	}
	invalid := command
	invalid.Args = []Arg{{Name: "服务名", SkipPrompt: true}}
	if err := Validate(invalid); err == nil || !strings.Contains(err.Error(), "默认值") {
		t.Fatal("a non-asked parameter without a default was accepted", err)
	}
	invalid.Args = []Arg{{Name: "bad\nname", Preset: "nginx"}}
	if err := Validate(invalid); err == nil {
		t.Fatal("a parameter name with a newline was accepted")
	}
}

func TestLegacyCommandsKeepDollarPlaceholdersLiteral(t *testing.T) {
	legacy := Command{ID: "legacy", Name: "legacy", Body: "awk '{print $1}' && echo $2"}
	if args := ArgsFor(legacy, OSDefault); len(args) != 0 {
		t.Fatal("a command without saved parameters invented inputs", args)
	}
	if got, err := Render(legacy, OSDefault, nil); err != nil || got != legacy.Body {
		t.Fatal("a legacy command was rewritten", got, err)
	}
	parameterized := legacy
	parameterized.Args = []Arg{{}}
	if args := ArgsFor(parameterized, OSDefault); len(args) != 2 {
		t.Fatal("edited commands did not get their placeholder inputs", args)
	}
}

func TestOptionsAndChoicePreferDetectionThenDefault(t *testing.T) {
	command := sample()
	if got := Options(command); strings.Join(got, ",") != "default,redhat,macos" {
		t.Fatal(got)
	}
	if got := ChooseSystem(command, OSRedHat); got != OSRedHat {
		t.Fatal("detected system ignored", got)
	}
	if got := ChooseSystem(command, OSArch); got != OSDefault {
		t.Fatal("unknown detection did not fall back to the default", got)
	}
	withoutDefault := Command{ID: "x", Name: "x", Variants: []Variant{{OS: OSDebian, Body: "apt update"}}}
	if got := ChooseSystem(withoutDefault, ""); got != OSDebian {
		t.Fatal("only variant not chosen", got)
	}
}

func TestValidateRejectsBadSnippetsAndAcceptsVariants(t *testing.T) {
	if err := Validate(sample()); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Command){
		"empty name":       func(c *Command) { c.Name = " " },
		"empty body":       func(c *Command) { c.Body, c.Variants = "", nil },
		"control sequence": func(c *Command) { c.Body = "echo \x1b[2J" },
		"duplicate system": func(c *Command) { c.Variants = append(c.Variants, c.Variants[0]) },
		"unknown system":   func(c *Command) { c.Variants[0].OS = "plan9" },
		"bad preset":       func(c *Command) { c.Args = []Arg{{Type: TypeInt, Preset: "many"}} },
		"bad type":         func(c *Command) { c.Args = []Arg{{Type: "blob"}} },
		"too many args":    func(c *Command) { c.Body = "$33" },
	} {
		command := sample()
		mutate(&command)
		if err := Validate(command); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestAbsurdPlaceholderNumbersStayBounded(t *testing.T) {
	command := Command{ID: "x", Name: "x", Body: "echo $999999999", Args: []Arg{{}}}
	if got := len(ArgsFor(command, OSDefault)); got != MaxArgs {
		t.Fatal("argument list was not bounded", got)
	}
	if err := Validate(command); err == nil {
		t.Fatal("a placeholder beyond the limit was accepted")
	}
}

func TestSystemDetectionCoversCommonFamilies(t *testing.T) {
	for name, want := range map[string]string{
		"Ubuntu 24.04.1 LTS":     OSDebian,
		"Debian GNU/Linux 12":    OSDebian,
		"Fedora Linux 41":        OSRedHat,
		"Red Hat Enterprise 9.4": OSRedHat,
		"Rocky Linux 9":          OSRedHat,
		"openSUSE Leap 15.6":     OSSUSE,
		"Arch Linux":             OSArch,
		"Alpine Linux v3.21":     OSAlpine,
		"macOS 15.1":             OSMacOS,
		"Windows Server 2022":    OSWindows,
		"":                       "",
	} {
		if got := SystemFor(name); got != want {
			t.Fatalf("%q => %q, want %q", name, got, want)
		}
	}
}
