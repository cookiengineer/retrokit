package index

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
)

type cppScope struct {
	name string
	kind string // namespace, class, struct, union, enum
}

type cppIndexer struct {
	src     []byte
	symbols []Symbol
	path    string
}

// IndexCpp extracts declarations from C, C++ and Objective-C(++) sources.
func IndexCpp(path string, content []byte) []Symbol {
	parser := tree_sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_cpp.Language()))
	tree := parser.Parse(content, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	ix := &cppIndexer{src: content, path: path}
	ix.walk(tree.RootNode(), nil)
	SortSymbols(ix.symbols)
	return ix.symbols
}

func (ix *cppIndexer) walk(n *tree_sitter.Node, scope []cppScope) {
	if n == nil {
		return
	}
	switch n.Kind() {
	case "translation_unit", "declaration_list", "field_declaration_list", "linkage_specification", "template_declaration":
		for i := uint(0); i < n.NamedChildCount(); i++ {
			ix.walk(n.NamedChild(i), scope)
		}

	case "namespace_definition":
		name := ix.nodeName(n)
		if name != "" {
			ix.add(n, "namespace", qualify(scope, name))
			scope = append(scope, cppScope{name: name, kind: "namespace"})
		}
		body := n.ChildByFieldName("body")
		if body != nil {
			ix.walk(body, scope)
		} else {
			for i := uint(0); i < n.NamedChildCount(); i++ {
				child := n.NamedChild(i)
				if child.Kind() == "declaration_list" {
					ix.walk(child, scope)
				}
			}
		}

	case "class_specifier", "struct_specifier", "union_specifier":
		name := ix.nodeName(n)
		kind := n.Kind()
		switch kind {
		case "class_specifier":
			kind = "class"
		case "struct_specifier":
			kind = "struct"
		default:
			kind = "union"
		}
		if name != "" {
			ix.add(n, kind, qualify(scope, name))
			scope = append(scope, cppScope{name: name, kind: kind})
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child.Kind() == "field_declaration_list" || child.Kind() == "declaration_list" {
				ix.walk(child, scope)
			}
		}

	case "enum_specifier":
		name := ix.nodeName(n)
		enumScope := scope
		if name != "" {
			ix.add(n, "enum", qualify(scope, name))
			enumScope = append(enumScope, cppScope{name: name, kind: "enum"})
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child.Kind() == "enumerator_list" {
				for j := uint(0); j < child.NamedChildCount(); j++ {
					enumerator := child.NamedChild(j)
					if enumerator.Kind() != "enumerator" {
						continue
					}
					valueName := ix.nodeName(enumerator)
					if valueName != "" {
						ix.add(enumerator, "enum_value", qualify(enumScope, valueName))
					}
				}
			}
		}

	case "function_definition":
		declarator := n.ChildByFieldName("declarator")
		name := ix.declaratorName(declarator)
		if name != "" {
			qualified := qualify(scope, name)
			kind := "function"
			if isMethodName(scope, qualified) {
				kind = "method"
			}
			ix.add(n, kind, qualified)
		}
		// Do not descend into function bodies: local variables and local
		// classes are not part of the public surface we track.

	case "declaration":
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			switch child.Kind() {
			case "function_declarator":
				name := ix.declaratorName(child)
				if name != "" {
					kind := "function"
					if scopeHasClass(scope) {
						kind = "method"
					}
					ix.add(n, kind, qualify(scope, name))
				}
			case "class_specifier", "struct_specifier", "union_specifier", "enum_specifier":
				ix.walk(child, scope)
			}
		}

	case "field_declaration":
		hasFunction := false
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child.Kind() == "function_declarator" {
				hasFunction = true
				if name := ix.declaratorName(child); name != "" {
					ix.add(n, "method", qualify(scope, name))
				}
			}
		}
		if !hasFunction {
			for i := uint(0); i < n.NamedChildCount(); i++ {
				child := n.NamedChild(i)
				if child.Kind() == "field_identifier" {
					ix.add(n, "field", qualify(scope, text(ix.src, child)))
				}
			}
		}

	case "type_definition":
		name := ""
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child.Kind() == "type_identifier" {
				name = text(ix.src, child)
			}
		}
		if name != "" {
			ix.add(n, "typedef", qualify(scope, name))
		}

	case "alias_declaration":
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child.Kind() == "type_identifier" {
				ix.add(n, "typedef", qualify(scope, text(ix.src, child)))
				break
			}
		}

	case "preproc_def", "preproc_function_def":
		name := ix.nodeName(n)
		if name != "" {
			ix.add(n, "macro", name)
		}
	}
}

func (ix *cppIndexer) add(n *tree_sitter.Node, kind, name string) {
	if n == nil || name == "" {
		return
	}
	ix.symbols = append(ix.symbols, Symbol{
		Name:      name,
		Kind:      kind,
		Path:      ix.path,
		Start:     int(n.StartByte()),
		End:       int(n.EndByte()),
		StartLine: int(n.StartPosition().Row) + 1,
		EndLine:   int(n.EndPosition().Row) + 1,
	})
}

func (ix *cppIndexer) nodeName(n *tree_sitter.Node) string {
	if n == nil {
		return ""
	}
	if field := n.ChildByFieldName("name"); field != nil {
		return text(ix.src, field)
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		switch child.Kind() {
		case "identifier", "type_identifier", "namespace_identifier", "field_identifier":
			return text(ix.src, child)
		}
	}
	return ""
}

func (ix *cppIndexer) declaratorName(n *tree_sitter.Node) string {
	if n == nil {
		return ""
	}
	switch n.Kind() {
	case "identifier", "field_identifier", "type_identifier", "destructor_name", "operator_name":
		return text(ix.src, n)
	case "qualified_identifier":
		return text(ix.src, n)
	case "function_declarator", "pointer_declarator", "reference_declarator", "array_declarator", "parenthesized_declarator":
		if decl := n.ChildByFieldName("declarator"); decl != nil {
			return ix.declaratorName(decl)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child.Kind() == "parameter_list" || child.Kind() == "type_qualifier" {
				continue
			}
			if name := ix.declaratorName(child); name != "" {
				return name
			}
		}
	case "template_function", "template_method":
		if name := n.ChildByFieldName("name"); name != nil {
			return text(ix.src, name)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child.Kind() == "identifier" || child.Kind() == "qualified_identifier" {
				return text(ix.src, child)
			}
		}
	}
	return ""
}

func scopeHasClass(scope []cppScope) bool {
	for _, entry := range scope {
		if entry.kind == "class" || entry.kind == "struct" || entry.kind == "union" {
			return true
		}
	}
	return false
}

func isMethodName(scope []cppScope, qualified string) bool {
	if scopeHasClass(scope) {
		return true
	}
	prefix := scopePrefix(scope)
	if prefix != "" {
		rest := strings.TrimPrefix(qualified, prefix+"::")
		return strings.Contains(rest, "::")
	}
	return strings.Contains(qualified, "::")
}

func scopePrefix(scope []cppScope) string {
	parts := make([]string, 0, len(scope))
	for _, entry := range scope {
		parts = append(parts, entry.name)
	}
	return strings.Join(parts, "::")
}

func qualify(scope []cppScope, name string) string {
	if name == "" {
		return ""
	}
	prefix := scopePrefix(scope)
	if prefix == "" {
		return name
	}
	if name == prefix || strings.HasPrefix(name, prefix+"::") {
		return name
	}
	return prefix + "::" + name
}

func text(src []byte, n *tree_sitter.Node) string {
	if n == nil {
		return ""
	}
	start := int(n.StartByte())
	end := int(n.EndByte())
	if start < 0 || end > len(src) || start > end {
		return ""
	}
	return string(src[start:end])
}
