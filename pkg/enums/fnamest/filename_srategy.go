package fnamest

//go:generate go-enum --values --names --noprefix --flag --nocase

// FnameST
/* ENUM(
default, message, template
) */
type FnameST string

var fnameSTDisplay = map[FnameST]map[string]string{
	Default:  {"en": "Default"},
	Message:  {"en": "Generate from Message First"},
	Template: {"en": "Template"},
}

func GetDisplay(st FnameST, lang string) string {
	if display, ok := fnameSTDisplay[st]; ok {
		if str, ok := display[lang]; ok {
			return str
		}
	}
	return fnameSTDisplay[st]["en"]
}
