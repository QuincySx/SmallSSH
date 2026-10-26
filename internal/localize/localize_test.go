package localize

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
)

func readMessages(t *testing.T, tag string) map[string]string {
	t.Helper()
	data, err := messages.ReadFile(tag + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	delete(result, "@@locale")
	return result
}

// The app uses Go printf arguments after translation. This also includes %%
// so translators cannot accidentally turn a literal percent into an argument.
var printfArgument = regexp.MustCompile(`%(?:\[[0-9]+\])?[+#\- 0]*(?:[0-9]+|\*)?(?:\.(?:[0-9]+|\*))?[vTtbcdoOqxXUeEfFgGspw%]`)

func TestCatalogCompleteAndStrictlyValid(t *testing.T) {
	source := readMessages(t, "zh-Hans")
	english := readMessages(t, "en")
	if len(source) != len(english) {
		t.Fatalf("catalog key counts differ: zh-Hans=%d en=%d", len(source), len(english))
	}
	for key, original := range source {
		translated, ok := english[key]
		if !ok || strings.TrimSpace(translated) == "" {
			t.Errorf("missing English translation for %q", key)
			continue
		}
		var parsed [2]*i18n.Message
		for i, value := range []string{original, translated} {
			message, err := i18n.ParseMessage(value)
			if err != nil {
				t.Errorf("%q message %d is invalid ICU: %v", key, i, err)
				continue
			}
			parsed[i] = message
		}
		if parsed[0] == nil || parsed[1] == nil {
			continue
		}
		args := func(message *i18n.Message) []string {
			var result []string
			for _, arg := range message.Args() {
				result = append(result, arg.Name+":"+arg.Kind)
			}
			slices.Sort(result)
			return result
		}
		if !reflect.DeepEqual(args(parsed[0]), args(parsed[1])) {
			t.Errorf("%q ICU arguments differ: %v / %v", key, args(parsed[0]), args(parsed[1]))
		}
		if !reflect.DeepEqual(printfArgument.FindAllString(original, -1), printfArgument.FindAllString(translated, -1)) {
			t.Errorf("%q printf arguments differ", key)
		}
		if !strings.HasPrefix(key, "mygo.") && parsed[0].Format("zh-Hans", nil) != key {
			t.Errorf("Chinese source did not round trip: %q", key)
		}
	}
	for key := range english {
		if _, ok := source[key]; !ok {
			t.Errorf("English key has no source: %q", key)
		}
	}
}

func TestTextUsesOfficialLocaleMatchingAndPreservesUnknownInput(t *testing.T) {
	for _, test := range []struct{ locale, source, want string }{
		{"", "保存", "保存"},
		{"zh-Hans", "保存", "保存"},
		{"zh-CN", "保存", "保存"},
		{"en", "保存", "Save"},
		{"en-GB", "保存", "Save"},
		{"fr", "保存", "保存"},
		{"en", "用户 %.1f%% · 系统 %.1f%% · Guest %.1f%% · I/O 等待 %.1f%% · Steal %.1f%%", "User %.1f%% · System %.1f%% · Guest %.1f%% · I/O wait %.1f%% · Steal %.1f%%"},
		{"en", "mygo.copy", "Copy"},
		{"zh-Hans", "mygo.copy", "复制"},
		{"en", "mygo.paste", "Paste"},
		{"zh-Hans", "mygo.clear", "清除"},
		{"en", "user-host /路径/{name}'%s", "user-host /路径/{name}'%s"},
	} {
		if got := Text(test.locale, test.source); got != test.want {
			t.Errorf("Text(%q, %q) = %q, want %q", test.locale, test.source, got, test.want)
		}
	}
	if Current("保存") != Text(i18n.Locale(), "保存") {
		t.Fatal("Current did not use MyGo's current locale")
	}
}

func TestDesktopLiteralTranslationSourcesExist(t *testing.T) {
	source := readMessages(t, "zh-Hans")
	files := token.NewFileSet()
	err := filepath.WalkDir("../desktop", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(files, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			index := -1
			switch name := call.Fun.(type) {
			case *ast.Ident:
				if name.Name == "tr" && len(call.Args) == 2 {
					index = 1
				}
			case *ast.SelectorExpr:
				pkg, ok := name.X.(*ast.Ident)
				if ok && pkg.Name == "localize" {
					if name.Sel.Name == "Current" && len(call.Args) == 1 {
						index = 0
					} else if name.Sel.Name == "Text" && len(call.Args) == 2 {
						index = 1
					}
				}
			}
			if index < 0 {
				return true
			}
			literal, ok := call.Args[index].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			key, err := strconv.Unquote(literal.Value)
			if err == nil {
				if _, ok := source[key]; !ok {
					t.Errorf("%s: untranslated source %q", files.Position(literal.Pos()), key)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
