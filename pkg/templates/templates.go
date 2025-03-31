package templates

import (
	"bytes"
	"embed"
	"strings"
	"text/template"
)

var (
	//go:embed templates/*
	content embed.FS
)

func LoadAndExecute(templateName string, data interface{}) (str string, err error) {
	var tplFunctions = template.FuncMap{
		"UpperCase":    strings.ToUpper,
		"LowerCase":    strings.ToLower,
		"ListAsString": func(elems []string) string { return strings.Join(elems, ",") },
	}

	tpl, err := template.New(templateName).Funcs(tplFunctions).ParseFS(content, "templates/"+templateName, "templates/common/*.xml")
	if err != nil {
		return
	}

	buffIspn := new(bytes.Buffer)
	if err = tpl.Execute(buffIspn, data); err != nil {
		return
	}
	return buffIspn.String(), nil
}
