package snippet

import "testing"

func TestParameterPromptAnnotatesOnlyConfiguredPlaceholders(t *testing.T) {
	c := Command{Body: "pct config $1 && echo ${2} $HOME", Args: []Arg{{Name: "ContainerID"}, {Name: "label"}}}
	if got := PromptBody(c, OSDefault); got != "pct config [$1 · ContainerID] && echo [${2} · label] $HOME" {
		t.Fatal(got)
	}
	if got := PromptBody(Command{Body: "awk '{print $1}'"}, OSDefault); got != "awk '{print $1}'" {
		t.Fatal("legacy shell fields were annotated as parameters", got)
	}
	c.History = []InputHistory{{Values: []string{"private"}}}
	copy := c.Clone()
	copy.History[0].Values[0] = "changed"
	if c.History[0].Values[0] != "private" {
		t.Fatal("command clone shared history values")
	}
}
