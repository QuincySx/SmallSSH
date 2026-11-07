package snippet

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const MaxInputHistory = 10

// InputHistory lives only in the encrypted owner vault. A target is a digest of
// the selected server/account/fingerprint/container, never a display label.
type InputHistory struct {
	Parameter  int      `json:"parameter"`
	System     string   `json:"system"`
	Target     string   `json:"target"`
	Definition string   `json:"definition"`
	Updated    int64    `json:"updated"`
	Values     []string `json:"values"`
}

// InputDefinition prevents old input from being offered after the command or
// the meaning/type of this parameter changes. Names of other parameters and
// display metadata do not change its identity.
func InputDefinition(command Command, system string, parameter int) string {
	args := ArgsFor(command, system)
	body, ok := BodyFor(command, system)
	if !ok || parameter < 0 || parameter >= len(args) || args[parameter].NoHistory || args[parameter].SkipPrompt {
		return ""
	}
	data, _ := json.Marshal(struct {
		Body, Name, Type string
		Parameter        int
	}{body, args[parameter].Name, NormalizeType(args[parameter].Type), parameter})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// PromptBody annotates placeholders for display only; Render remains the
// source of the exact bytes sent to a terminal.
func PromptBody(command Command, system string) string {
	body, _ := BodyFor(command, system)
	args := ArgsFor(command, system)
	return placeholder.ReplaceAllStringFunc(body, func(token string) string {
		positions := placeholders(token)
		if len(positions) != 1 || positions[0] > len(args) {
			return token
		}
		name := args[positions[0]-1].Name
		if name == "" {
			return "[" + token + "]"
		}
		return "[" + token + " · " + name + "]"
	})
}
