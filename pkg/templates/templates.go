package templates

import (
	"bytes"
	"strings"
	"text/template"

	rice "github.com/GeertJohan/go.rice"
)

var defaultTplFunctions = template.FuncMap{
	"UpperCase":    strings.ToUpper,
	"LowerCase":    strings.ToLower,
	"ListAsString": func(elems []string) string { return strings.Join(elems, ",") },
}

func LoadAndExecute(templateName string, funcMap template.FuncMap, data interface{}) (str string, err error) {
	if funcMap == nil {
		funcMap = make(map[string]interface{}, len(defaultTplFunctions))
	}
	for key, value := range defaultTplFunctions {
		funcMap[key] = value
	}

	box, err := rice.FindBox("templates")
	if err != nil {
		return
	}

	var tplStr string
	if tplStr, err = box.String(templateName); err != nil {
		return
	}

	tpl, err := template.New(templateName).Funcs(funcMap).Parse(tplStr)
	if err != nil {
		return
	}

	buffIspn := new(bytes.Buffer)
	if err = tpl.Execute(buffIspn, data); err != nil {
		return
	}
	return buffIspn.String(), nil
}
