package cmd

import (
	"bytes"
	_ "embed"
	"fmt"
	"text/template"
)

//go:embed templates/wakeup_hammerspoon.zsh.tmpl
var wakeupHammerspoonTemplate string

//go:embed templates/hammerspoon_module.lua.tmpl
var hammerspoonModuleTemplate string

//go:embed templates/oci_access_notifier.swift.tmpl
var ociAccessNotifierSwiftTemplate string

//go:embed templates/oci_access_info.plist.tmpl
var ociAccessInfoPlistTemplate string

func renderWakeupScriptWithHammerspoon(ociContextBin, daemonLabel string) string {
	return renderTextTemplate(
		"wakeup_hammerspoon.zsh.tmpl",
		wakeupHammerspoonTemplate,
		struct {
			OciContextBinQuoted string
			DaemonLabelQuoted   string
		}{
			OciContextBinQuoted: shellQuote(ociContextBin),
			DaemonLabelQuoted:   shellQuote(daemonLabel),
		},
	)
}

func renderHammerspoonModule() string {
	return renderTextTemplate("hammerspoon_module.lua.tmpl", hammerspoonModuleTemplate, nil)
}

func renderOCIAccessNotifierSwift() string {
	return renderTextTemplate("oci_access_notifier.swift.tmpl", ociAccessNotifierSwiftTemplate, nil)
}

func renderOCIAccessInfoPlist() string {
	return renderTextTemplate("oci_access_info.plist.tmpl", ociAccessInfoPlistTemplate, nil)
}

func renderTextTemplate(name, tmpl string, data interface{}) string {
	t, err := template.New(name).Option("missingkey=error").Parse(tmpl)
	if err != nil {
		panic(fmt.Sprintf("parse template %s: %v", name, err))
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		panic(fmt.Sprintf("execute template %s: %v", name, err))
	}
	return b.String()
}
