// Package snippet defines the owner's saved shell commands: a default body,
// optional bodies for a particular system, and the typed placeholders that a
// runner fills before the command is sent to a terminal.
//
// It never sends anything and holds no credentials; the encrypted vault stores
// the values and the desktop renders them.
package snippet

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	MaxBody     = 32 << 10
	MaxArgs     = 32
	MaxVariants = 16
)

// Stable system keys are stored in the vault; labels stay in the desktop so
// the same entry remains readable in either language.
const (
	OSDefault = "default"
	OSDebian  = "debian"
	OSRedHat  = "redhat"
	OSSUSE    = "suse"
	OSArch    = "arch"
	OSAlpine  = "alpine"
	OSMacOS   = "macos"
	OSWindows = "windows"
)

const (
	TypeText   = "string"
	TypeInt    = "int"
	TypeNumber = "number"
)

// An Arg describes one placeholder by its position: Args[0] is $1. Name is
// only a label for the owner; the command still uses the positional token.
// SkipPrompt uses Preset without asking; when false the run dialog shows the
// field prefilled with Preset.
type Arg struct {
	Type       string `json:"type,omitempty"`
	Name       string `json:"name,omitempty"`
	Preset     string `json:"preset,omitempty"`
	SkipPrompt bool   `json:"skipPrompt,omitempty"`
	NoHistory  bool   `json:"noHistory,omitempty"`
}

// A Variant replaces the default body on one system.
type Variant struct {
	OS   string `json:"os"`
	Body string `json:"body"`
}

// Command shares the encrypted vault payload; bodies may contain secrets.
type Command struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Group    string         `json:"group"`
	Body     string         `json:"body"`
	Variants []Variant      `json:"variants,omitempty"`
	Args     []Arg          `json:"args,omitempty"`
	History  []InputHistory `json:"parameterHistory,omitempty"`
	// SkipConfirm is per command: dropping the reminder on one command must
	// not silence the others.
	SkipConfirm bool `json:"skipConfirm,omitempty"`
}

// Clone gives an editor or vault its own parameter and system variant slices.
func (c Command) Clone() Command {
	c.Variants = append([]Variant(nil), c.Variants...)
	c.Args = append([]Arg(nil), c.Args...)
	c.History = append([]InputHistory(nil), c.History...)
	for i := range c.History {
		c.History[i].Values = append([]string(nil), c.History[i].Values...)
	}
	return c
}

var placeholder = regexp.MustCompile(`\$(?:\{([1-9][0-9]*)\}|([1-9][0-9]*))`)

// SystemKeys lists the systems a variant may target, in menu order. The
// default body is not a variant.
func SystemKeys() []string {
	return []string{OSDebian, OSRedHat, OSSUSE, OSArch, OSAlpine, OSMacOS, OSWindows}
}

func knownSystem(key string) bool {
	for _, candidate := range SystemKeys() {
		if candidate == key {
			return true
		}
	}
	return false
}

func KnownType(value string) bool {
	return value == "" || value == TypeText || value == TypeInt || value == TypeNumber
}

func NormalizeType(value string) string {
	if value == TypeInt || value == TypeNumber {
		return value
	}
	return TypeText
}

// placeholders returns the distinct placeholder numbers of body, in order.
func placeholders(body string) []int {
	var out []int
	seen := map[int]bool{}
	for _, match := range placeholder.FindAllStringSubmatch(body, -1) {
		number, err := strconv.Atoi(match[1] + match[2])
		if err == nil && !seen[number] {
			seen[number] = true
			out = append(out, number)
		}
	}
	return out
}

// BodyFor returns the body that runs on system, and whether one exists. An
// explicit variant wins; any other system falls back to the default body.
func BodyFor(command Command, system string) (string, bool) {
	if system != "" && system != OSDefault {
		for _, variant := range command.Variants {
			if variant.OS == system && strings.TrimSpace(variant.Body) != "" {
				return variant.Body, true
			}
		}
	}
	return command.Body, strings.TrimSpace(command.Body) != ""
}

// Options lists the systems this command can run on. Empty variant bodies are
// placeholders in the editor, not choices at run time.
func Options(command Command) []string {
	var out []string
	seen := map[string]bool{}
	if strings.TrimSpace(command.Body) != "" {
		out = append(out, OSDefault)
		seen[OSDefault] = true
	}
	for _, system := range SystemKeys() {
		for _, variant := range command.Variants {
			if variant.OS == system && strings.TrimSpace(variant.Body) != "" && !seen[system] {
				out = append(out, system)
				seen[system] = true
			}
		}
	}
	return out
}

// ChooseSystem prefers the detected system, then the default body, then the
// first available variant.
func ChooseSystem(command Command, detected string) string {
	options := Options(command)
	if detected != "" {
		for _, option := range options {
			if option == detected {
				return detected
			}
		}
	}
	if strings.TrimSpace(command.Body) != "" {
		return OSDefault
	}
	if len(options) > 0 {
		return options[0]
	}
	return ""
}

// PresetValues returns the configured defaults for the body chosen for
// system, used when a parameter does not need a prompt.
func PresetValues(command Command, system string) []string {
	args := ArgsFor(command, system)
	values := make([]string, len(args))
	for i, arg := range args {
		values[i] = arg.Preset
	}
	return values
}

// ArgsFor returns the definitions for the placeholders in the body chosen for
// system. Undefined positions default to text without a preset, and extra
// definitions beyond the body are not shown.
func ArgsFor(command Command, system string) []Arg {
	// A command saved before placeholders existed has no definitions. Its
	// literal $1 (an awk field, a shell argument) must keep working until the
	// owner edits the body or explicitly turns the placeholders into inputs.
	if len(command.Args) == 0 {
		return nil
	}
	body, ok := BodyFor(command, system)
	if !ok {
		return nil
	}
	n := 0
	for _, number := range placeholders(body) {
		n = max(n, number)
	}
	if n > MaxArgs {
		n = MaxArgs
	}
	out := make([]Arg, n)
	for i := range out {
		if i < len(command.Args) {
			out[i] = command.Args[i]
		}
		out[i].Type = NormalizeType(out[i].Type)
	}
	return out
}

// PlaceholderCount is the number of placeholders any body of the command uses.
func PlaceholderCount(command Command) int {
	n := bodyPlaceholders(command.Body)
	for _, variant := range command.Variants {
		n = max(n, bodyPlaceholders(variant.Body))
	}
	return n
}

func bodyPlaceholders(body string) int {
	n := 0
	for _, number := range placeholders(body) {
		n = max(n, number)
	}
	return n
}

// ParseValue validates one filled value and returns the text to substitute.
// Numeric values lose surrounding whitespace; text is kept verbatim.
func ParseValue(arg Arg, value string) (string, error) {
	switch NormalizeType(arg.Type) {
	case TypeInt:
		value = strings.TrimSpace(value)
		if value == "" {
			return "", errors.New("需要整数")
		}
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return "", errors.New("需要整数")
		}
		return value, nil
	case TypeNumber:
		value = strings.TrimSpace(value)
		if value == "" {
			return "", errors.New("需要数字")
		}
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return "", errors.New("需要数字")
		}
		return value, nil
	default:
		return value, nil
	}
}

// Render validates the values and substitutes them into the body chosen for
// system. It is not a shell quoter: the text is inserted exactly as filled.
func Render(command Command, system string, values []string) (string, error) {
	body, ok := BodyFor(command, system)
	if !ok {
		return "", errors.New("该命令没有可用的内容")
	}
	args := ArgsFor(command, system)
	filled := make([]string, len(args))
	for i, arg := range args {
		value := ""
		if i < len(values) {
			value = values[i]
		}
		normalized, err := ParseValue(arg, value)
		if err != nil {
			return "", fmt.Errorf("参数 $%d：%w", i+1, err)
		}
		filled[i] = normalized
	}
	return placeholder.ReplaceAllStringFunc(body, func(token string) string {
		match := placeholder.FindStringSubmatch(token)
		number, err := strconv.Atoi(match[1] + match[2])
		if err != nil || number < 1 || number > len(filled) {
			return token
		}
		return filled[number-1]
	}), nil
}

// Validate keeps the vault payload bounded and rejects bodies the UI cannot
// edit safely. Callers save only commands that pass.
func Validate(command Command) error {
	if command.ID == "" || len(command.ID) > 128 {
		return errors.New("命令 ID 无效")
	}
	if len(command.Name) > 256 || strings.TrimSpace(command.Name) == "" || !utf8.ValidString(command.Name) || strings.ContainsAny(command.Name, "\x00\x1b\r\n") {
		return errors.New("请填写命令名称；名称最多 256 字节且不能包含换行")
	}
	if strings.TrimSpace(command.Group) != command.Group || len(command.Group) > 128 || !utf8.ValidString(command.Group) || strings.ContainsAny(command.Group, "\x00\x1b\r\n") {
		return errors.New("命令分类最多 128 字节，不能包含换行或首尾空格")
	}
	if len(command.Variants) > MaxVariants {
		return fmt.Errorf("系统版本最多 %d 个", MaxVariants)
	}
	if len(command.Body) > MaxBody || !utf8.ValidString(command.Body) || strings.ContainsAny(command.Body, "\x00\x1b") {
		return fmt.Errorf("命令内容最多 %d 字节，不允许控制序列", MaxBody/1024)
	}
	hasBody := strings.TrimSpace(command.Body) != ""
	seen := map[string]bool{}
	for _, variant := range command.Variants {
		if !knownSystem(variant.OS) {
			return errors.New("系统类型无效")
		}
		if seen[variant.OS] {
			return errors.New("同一系统只能有一个版本")
		}
		seen[variant.OS] = true
		if len(variant.Body) > MaxBody || !utf8.ValidString(variant.Body) || strings.ContainsAny(variant.Body, "\x00\x1b") {
			return fmt.Errorf("命令内容最多 %d 字节，不允许控制序列", MaxBody/1024)
		}
		hasBody = hasBody || strings.TrimSpace(variant.Body) != ""
	}
	if !hasBody {
		return errors.New("请填写命令内容")
	}
	if PlaceholderCount(command) > MaxArgs {
		return fmt.Errorf("占位符最多 %d 个", MaxArgs)
	}
	if len(command.Args) > MaxArgs {
		return fmt.Errorf("参数最多 %d 个", MaxArgs)
	}
	for i, arg := range command.Args {
		if !KnownType(arg.Type) {
			return fmt.Errorf("参数 $%d 的类型无效", i+1)
		}
		if len(arg.Name) > 64 || !utf8.ValidString(arg.Name) || strings.ContainsAny(arg.Name, "\x00\x1b\r\n") {
			return fmt.Errorf("参数 $%d 的名称最多 64 字节，不能包含控制字符", i+1)
		}
		if len(arg.Preset) > 4096 || !utf8.ValidString(arg.Preset) || strings.ContainsAny(arg.Preset, "\x00\x1b") {
			return fmt.Errorf("参数 $%d 的默认值无效", i+1)
		}
		if arg.SkipPrompt && strings.TrimSpace(arg.Preset) == "" {
			return fmt.Errorf("参数 $%d 关闭发送时填写后必须填写默认值", i+1)
		}
		if arg.Preset != "" {
			if _, err := ParseValue(arg, arg.Preset); err != nil {
				return fmt.Errorf("参数 $%d 的默认值%s", i+1, err)
			}
		}
	}
	return nil
}

// SystemFor maps a distribution's display name (PRETTY_NAME) to the family a
// variant targets. Unknown names keep the default body.
func SystemFor(name string) string {
	name = strings.ToLower(name)
	switch {
	case containsAny(name, "ubuntu", "debian", "raspbian", "linux mint", "pop!_os", "kali", "deepin", "zorin", "elementary os", "devuan"):
		return OSDebian
	case containsAny(name, "fedora", "red hat", "redhat", "rhel", "centos", "rocky", "alma", "oracle linux", "amazon linux"):
		return OSRedHat
	case containsAny(name, "opensuse", "suse"):
		return OSSUSE
	case containsAny(name, "arch linux", "archlinux", "manjaro", "endeavouros"):
		return OSArch
	case containsAny(name, "alpine"):
		return OSAlpine
	case containsAny(name, "macos", "mac os", "darwin"):
		return OSMacOS
	case containsAny(name, "windows"):
		return OSWindows
	}
	return ""
}

func containsAny(value string, options ...string) bool {
	for _, option := range options {
		if strings.Contains(value, option) {
			return true
		}
	}
	return false
}
