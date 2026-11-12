package docgen

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ensamuel7/dex/stdlib"
)

func TestEveryModuleDocumented(t *testing.T) {
	out, err := Generate(".")
	if err != nil {
		t.Fatal(err)
	}

	sections := map[string]string{}
	for _, m := range regexp.MustCompile(`(?s)<section id="([^"]+)" class="page-section">(.*?)</section>`).FindAllStringSubmatch(out, -1) {
		sections[m[1]] = m[2]
	}

	for name, mod := range stdlib.AllModules() {
		if moduleSummaries[name] == "" {
			t.Errorf("module %s has no entry in moduleSummaries", name)
		}
		page, ok := sections[modulePage(name)]
		if !ok {
			t.Errorf("module %s has no page %s", name, modulePage(name))
			continue
		}
		if !strings.Contains(out, "show('"+modulePage(name)+"')") {
			t.Errorf("module %s is not linked from the docs", name)
		}
		for fn := range mod.Funcs {
			if !strings.Contains(page, `<span class="module">`+name+`</span>.`+fn+`(`) {
				t.Errorf("%s.%s is not on the %s page", name, fn, modulePage(name))
			}
		}
	}
}

func TestFuncsRejectsUnknownNames(t *testing.T) {
	data := TemplateData{Modules: []ModuleData{{Name: "crypto", Funcs: []FuncData{{Module: "crypto", Name: "uuid"}}}}}
	if _, err := data.funcs("crypto", "uuid"); err != nil {
		t.Fatal(err)
	}
	if _, err := data.funcs("crypto", "nope"); err == nil {
		t.Error("an unknown function was accepted")
	}
	if _, err := data.funcs("nope"); err == nil {
		t.Error("an unknown module was accepted")
	}
}
