package gui

import (
	"fmt"
	"strings"
)

// trayZh is the tray menu in Chinese; the page has its own words
// (i18n.js), and these few are all the menu shows (#301).
var trayZh = map[string]string{
	"Open magpie":             "打开 magpie",
	"Version %s":              "版本 %s",
	"Restart to Update":       "重启以更新",
	"Restart to Update to %s": "重启以更新到 %s",
	"Quit magpie":             "退出 magpie",
}

// onLang relabels the tray menu when the Settings page changes the
// language; set by the process that has the tray.
var onLang func()

// trayLang is the language the tray menu is in, as the page picks its own:
// the setting, or with "system" (or none) the system's, Chinese for any zh.
func trayLang(pref string, system func() string) string {
	switch pref {
	case "en", "zh":
		return pref
	}
	if strings.HasPrefix(strings.ToLower(system()), "zh") {
		return "zh"
	}
	return "en"
}

// trayText is a menu line in the language, English where it has none.
func trayText(lang, key string, args ...any) string {
	if lang == "zh" {
		if s, ok := trayZh[key]; ok {
			key = s
		}
	}
	if len(args) == 0 {
		return key
	}
	return fmt.Sprintf(key, args...)
}

// trayLabels are the tray menu's lines: open, version, restart and quit.
type trayLabels struct{ open, version, restart, quit string }

// trayMenuLabels are the lines in the language; update is the version
// waiting for a restart, if there is one.
func trayMenuLabels(lang, version, update string) trayLabels {
	l := trayLabels{
		open:    trayText(lang, "Open magpie"),
		version: trayText(lang, "Version %s", version),
		restart: trayText(lang, "Restart to Update"),
		quit:    trayText(lang, "Quit magpie"),
	}
	if update != "" {
		l.restart = trayText(lang, "Restart to Update to %s", update)
	}
	return l
}

// envLang is the language the environment names: the first of LC_ALL,
// LC_MESSAGES, LANG and LANGUAGE set to one.
func envLang(getenv func(string) string) string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if v := getenv(k); v != "" && v != "C" && v != "POSIX" && !strings.HasPrefix(v, "C.") {
			return v
		}
	}
	return ""
}

// firstAppleLang is the first entry `defaults read -g AppleLanguages`
// prints: ( "zh-Hans-CN", "en-US" ), one to a line, quoted or not.
func firstAppleLang(out string) string {
	for _, line := range strings.Split(out, "\n") {
		s := strings.Trim(strings.TrimSpace(line), `",`)
		if s != "" && s != "(" && s != ")" {
			return s
		}
	}
	return ""
}
