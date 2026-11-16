package index

import "strings"

type idlToken struct {
	text       string
	start, end int
	line       int
}

// IndexIDL extracts WebIDL definitions and members. WebIDL is small enough
// that a dedicated lexer is more predictable than trying to reuse a C++
// grammar.
func IndexIDL(path string, content []byte) []Symbol {
	tokens := lexIDL(string(content))
	var symbols []Symbol
	for i := 0; i < len(tokens); {
		tok := tokens[i].text

		if tok == "[" {
			i = skipBalanced(tokens, i, "[", "]")
			continue
		}
		if tok == "partial" {
			i++
			continue
		}

		switch tok {
		case "interface", "dictionary", "namespace":
			start := i
			i++
			if i < len(tokens) && tokens[i].text == "mixin" {
				i++
			}
			if i >= len(tokens) || !isIDLIdent(tokens[i].text) {
				continue
			}
			name := tokens[i].text
			nameToken := tokens[i]
			i++
			end := nameToken.end
			if i < len(tokens) && tokens[i].text == "{" {
				close := matchingBrace(tokens, i)
				if close < 0 {
					continue
				}
				end = tokens[close].end
				if tok != "namespace" {
					symbols = append(symbols, makeIDLSymbol(path, tok, name, tokens[start].start, end, tokens[start].line, tokens[close].line))
					symbols = append(symbols, idlMembers(path, tokens, i, close, name)...)
				} else {
					symbols = append(symbols, makeIDLSymbol(path, tok, name, tokens[start].start, end, tokens[start].line, tokens[close].line))
				}
				i = close + 1
			} else {
				// forward declaration
				symbols = append(symbols, makeIDLSymbol(path, tok, name, tokens[start].start, end, tokens[start].line, nameToken.line))
			}

		case "enum":
			start := i
			i++
			if i >= len(tokens) || !isIDLIdent(tokens[i].text) {
				continue
			}
			name := tokens[i].text
			i++
			if i >= len(tokens) || tokens[i].text != "{" {
				continue
			}
			close := matchingBrace(tokens, i)
			if close < 0 {
				continue
			}
			symbols = append(symbols, makeIDLSymbol(path, "enum", name, tokens[start].start, tokens[close].end, tokens[start].line, tokens[close].line))
			for j := i + 1; j < close; j++ {
				value := tokens[j].text
				if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') {
					value = strings.Trim(value, "\"'")
				}
				if isIDLIdent(value) || (value != tokens[j].text && value != "") {
					symbols = append(symbols, makeIDLSymbol(path, "enum_value", name+"::"+value, tokens[j].start, tokens[j].end, tokens[j].line, tokens[j].line))
				}
			}
			i = close + 1

		case "callback":
			start := i
			i++
			if i < len(tokens) && tokens[i].text == "interface" {
				i++
			}
			if i >= len(tokens) || !isIDLIdent(tokens[i].text) {
				continue
			}
			name := tokens[i].text
			semi := findToken(tokens, i, ";")
			if semi < 0 {
				continue
			}
			symbols = append(symbols, makeIDLSymbol(path, "callback", name, tokens[start].start, tokens[semi].end, tokens[start].line, tokens[semi].line))
			i = semi + 1

		case "typedef":
			start := i
			semi := findToken(tokens, i, ";")
			if semi < 0 {
				continue
			}
			name := ""
			for j := semi - 1; j > i; j-- {
				if isIDLIdent(tokens[j].text) {
					name = tokens[j].text
					break
				}
			}
			if name != "" {
				symbols = append(symbols, makeIDLSymbol(path, "typedef", name, tokens[start].start, tokens[semi].end, tokens[start].line, tokens[semi].line))
			}
			i = semi + 1

		default:
			i++
		}
	}
	return symbols
}

func idlMembers(path string, tokens []idlToken, open, close int, owner string) []Symbol {
	var symbols []Symbol
	stmtStart := open + 1
	for i := open + 1; i < close; i++ {
		if tokens[i].text == ";" {
			symbols = append(symbols, idlStatement(path, tokens[stmtStart:i+1], owner)...)
			stmtStart = i + 1
		}
	}
	if stmtStart < close {
		symbols = append(symbols, idlStatement(path, tokens[stmtStart:close], owner)...)
	}
	return symbols
}

func idlStatement(path string, tokens []idlToken, owner string) []Symbol {
	if len(tokens) == 0 {
		return nil
	}
	// Ignore attributes such as [Exposed=Window].
	for len(tokens) > 0 && tokens[0].text != "" && (tokens[0].text == "[" || tokens[0].text == "]") {
		tokens = tokens[1:]
	}
	if len(tokens) == 0 {
		return nil
	}

	nameEnd := len(tokens)
	if eq := findToken(tokens, 0, "="); eq >= 0 {
		nameEnd = eq
	}

	name := ""
	kind := "idl_member"
	for i, tok := range tokens {
		switch tok.text {
		case "attribute":
			name = lastIdentifier(tokens, i+1, nameEnd)
			kind = "idl_attribute"
		case "const":
			name = lastIdentifier(tokens, i+1, nameEnd)
			kind = "idl_const"
		case "readonly":
			// handled by the attribute keyword
		case "constructor":
			name = "constructor"
		}
		if tok.text == "(" && i > 0 {
			prev := tokens[i-1].text
			if isIDLIdent(prev) {
				name = prev
				kind = "idl_operation"
			}
		}
	}
	if name == "" {
		name = lastIdentifier(tokens, 0, nameEnd)
	}
	if name == "" || !isIDLIdent(name) {
		return nil
	}

	start := tokens[0].start
	end := tokens[len(tokens)-1].end
	return []Symbol{makeIDLSymbol(path, kind, owner+"::"+name, start, end, tokens[0].line, tokens[len(tokens)-1].line)}
}

func makeIDLSymbol(path, kind, name string, start, end, startLine, endLine int) Symbol {
	return Symbol{Name: name, Kind: kind, Path: path, Start: start, End: end, StartLine: startLine, EndLine: endLine}
}

func lexIDL(src string) []idlToken {
	var tokens []idlToken
	line := 1
	for i := 0; i < len(src); {
		c := src[i]
		if c == '\n' {
			line++
			i++
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' {
			i++
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				if src[i] == '\n' {
					line++
				}
				i++
			}
			i += 2
			continue
		}
		if c == '"' || c == '\'' {
			start := i
			quote := c
			i++
			for i < len(src) && src[i] != quote {
				if src[i] == '\\' && i+1 < len(src) {
					i++
				}
				if src[i] == '\n' {
					line++
				}
				i++
			}
			if i < len(src) {
				i++
			}
			tokens = append(tokens, idlToken{text: src[start:i], start: start, end: i, line: line})
			continue
		}
		if isIdentStart(c) {
			start := i
			for i < len(src) && isIdentPart(src[i]) {
				i++
			}
			tokens = append(tokens, idlToken{text: src[start:i], start: start, end: i, line: line})
			continue
		}
		if c >= '0' && c <= '9' {
			start := i
			for i < len(src) && (isIdentPart(src[i]) || src[i] == '.') {
				i++
			}
			tokens = append(tokens, idlToken{text: src[start:i], start: start, end: i, line: line})
			continue
		}
		tokens = append(tokens, idlToken{text: string(c), start: i, end: i + 1, line: line})
		i++
	}
	return tokens
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9'
}

func isIDLIdent(s string) bool {
	if s == "" {
		return false
	}
	if !isIdentStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isIdentPart(s[i]) {
			return false
		}
	}
	switch s {
	case "interface", "dictionary", "enum", "callback", "typedef", "namespace",
		"partial", "mixin", "attribute", "readonly", "const", "static", "required",
		"getter", "setter", "deleter", "constructor", "stringifier", "iterable",
		"maplike", "setlike", "async", "Promise", "sequence", "record", "USVString",
		"DOMString", "ByteString", "undefined", "any", "boolean", "byte", "octet",
		"short", "long", "float", "double", "unsigned", "unrestricted":
		return false
	}
	return true
}

func skipBalanced(tokens []idlToken, i int, open, close string) int {
	depth := 0
	for ; i < len(tokens); i++ {
		switch tokens[i].text {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return i
}

func matchingBrace(tokens []idlToken, open int) int {
	return skipBalanced(tokens, open, "{", "}") - 1
}

func findToken(tokens []idlToken, start int, want string) int {
	for i := start; i < len(tokens); i++ {
		if tokens[i].text == want {
			return i
		}
	}
	return -1
}

func lastIdentifier(tokens []idlToken, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(tokens) {
		end = len(tokens)
	}
	for i := end - 1; i >= start; i-- {
		if isIDLIdent(tokens[i].text) {
			return tokens[i].text
		}
	}
	return ""
}
