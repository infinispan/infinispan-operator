package templates

import (
	"bytes"
	"strings"
	"text/template"

	rice "github.com/GeertJohan/go.rice"
)

func LoadAndExecute(templateName string, funcMap template.FuncMap, data interface{}) (str string, err error) {
	var tplFunctions = template.FuncMap{
		"UpperCase":    strings.ToUpper,
		"LowerCase":    strings.ToLower,
		"ListAsString": func(elems []string) string { return strings.Join(elems, ",") },
	}

	for key, value := range funcMap {
		tplFunctions[key] = value
	}

	box, err := rice.FindBox("templates")
	if err != nil {
		return
	}

	var tplStr string
	if tplStr, err = box.String(templateName); err != nil {
		return
	}

	tpl, err := template.New(templateName).Funcs(tplFunctions).Parse(tplStr)
	if err != nil {
		return
	}

	buffIspn := new(bytes.Buffer)
	if err = tpl.Execute(buffIspn, data); err != nil {
		return
	}
	return buffIspn.String(), nil
}
