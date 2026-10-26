package desktop

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *app) languageSettings(c *ui.Context) {
	a.languageSettingsWith(c, mygo.App.SetLocale)
}

func (a *app) languageSettingsWith(c *ui.Context, apply func(string)) {
	choices := []string{"", "zh-Hans", "en"}
	labels := []string{tr(c, "跟随系统"), "简体中文", "English"}
	selected := labels[0]
	for i, choice := range choices {
		if choice == a.languageChoice {
			selected = labels[i]
		}
	}
	ui.Column(c).FillWidth().Padding(16, 16, 4, 16).Radius(8).Gap(0).
		Background(c.Theme().Surface).Label(tr(c, "语言设置")).Children(func() {
		settingsParameter(c, tr(c, "界面语言"), true, func() {
			if ui.Select(c, &selected, labels).Width(180).MinWidth(0).Label(tr(c, "界面语言选择")).Changed() {
				for i, label := range labels {
					if selected == label {
						if _, err := changeLanguageChoice(&a.languageChoice, choices[i], a.saveLayout, apply); err != nil {
							a.status = fmt.Sprintf(tr(c, "无法保存语言设置：%s"), err)
						}
						break
					}
				}
			}
		})
	})
}
