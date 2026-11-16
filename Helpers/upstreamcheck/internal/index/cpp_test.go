package index

import "testing"

func TestIndexCpp(t *testing.T) {
	src := []byte(`namespace WebCore {
class Foo {
public:
    Foo();
    void method(int x) const;
    int field;
};
void Foo::method(int x) const { }
enum class Color { Red, Green };
#define MACRO(x) ((x)+1)
int freeFunction();
}
`)
	symbols := IndexCpp("Foo.cpp", src)
	got := make(map[string]string)
	for _, sym := range symbols {
		got[sym.Name] = sym.Kind
	}

	expected := map[string]string{
		"WebCore":               "namespace",
		"WebCore::Foo":          "class",
		"WebCore::Foo::Foo":     "method",
		"WebCore::Foo::method":  "method",
		"WebCore::Foo::field":   "field",
		"WebCore::Color":        "enum",
		"WebCore::Color::Red":   "enum_value",
		"WebCore::Color::Green": "enum_value",
		"MACRO":                 "macro",
		"WebCore::freeFunction": "function",
	}
	for name, kind := range expected {
		if got[name] != kind {
			t.Errorf("symbol %q: got kind %q, want %q\nall symbols: %#v", name, got[name], kind, got)
		}
	}
}

func TestIndexCppRemovalSpan(t *testing.T) {
	src := []byte("class Foo {\n    void removeMe();\n    void keepMe();\n};\n")
	symbols := IndexCpp("Foo.h", src)
	for _, sym := range symbols {
		if sym.Name == "Foo::removeMe" {
			if sym.Start <= 0 || sym.End <= sym.Start || sym.End > len(src) {
				t.Fatalf("invalid span: %#v", sym)
			}
			return
		}
	}
	t.Fatalf("removeMe not indexed: %#v", symbols)
}
