package templates

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"text/template"
)

var (
	//go:embed templates/*
	content   embed.FS
	templates map[string]string
)

func init() {
	dirPath := "templates"
	dirEntries, err := fs.ReadDir(content, dirPath)
	if err != nil {
		panic(fmt.Errorf("unable to load templates: %w", err))
	}
	templates = make(map[string]string, len(dirEntries))
	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}

		filePath := filepath.Join(dirPath, entry.Name())
		file, err := fs.ReadFile(content, filePath)
		if err != nil {
			panic(fmt.Errorf("unable to load template '%s': %w", filePath, err))
		}
		templates[entry.Name()] = string(file)
	}
}

func LoadAndExecute(templateName string, funcMap template.FuncMap, data interface{}) (str string, err error) {
	var tplFunctions = template.FuncMap{
		"UpperCase":    strings.ToUpper,
		"LowerCase":    strings.ToLower,
		"ListAsString": func(elems []string) string { return strings.Join(elems, ",") },
	}

	for key, value := range funcMap {
		tplFunctions[key] = value
	}

	tplStr := templates[templateName]
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
