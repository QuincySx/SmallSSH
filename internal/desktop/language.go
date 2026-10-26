package desktop

import (
	"github.com/egoist/mygo/ui"
	"sshclient/internal/localize"
)

func tr(c *ui.Context, source string) string {
	if c != nil {
		return localize.Text(c.Locale(), source)
	}
	return localize.Current(source)
}

func validLanguageChoice(choice string) bool {
	return choice == "" || choice == "zh-Hans" || choice == "en"
}

func languageChoiceOrSystem(choice string) string {
	if validLanguageChoice(choice) {
		return choice
	}
	return ""
}

// Persist the selection mode even when following the system resolves to the
// same language as an explicit choice. MyGo only notifies effective changes.
// The callback keeps this state transition testable without touching native
// application preferences.
func changeLanguageChoice(current *string, next string, save func() error, apply func(string)) (bool, error) {
	if !validLanguageChoice(next) || *current == next {
		return false, nil
	}
	previous := *current
	*current = next
	if err := save(); err != nil {
		*current = previous
		return false, err
	}
	apply(next)
	return true, nil
}
