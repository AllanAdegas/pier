package stack

import (
	"sort"
	"strings"
)

// Variable kinds: who provides the value.
const (
	// VariableGenerated values are created by Pier (passwords, keys).
	VariableGenerated = "generated"
	// VariableAddress values come from the domain of a published service.
	VariableAddress = "address"
	// VariableUser values are the user's to define.
	VariableUser = "user"
)

// Variable is a variable a compose file interpolates.
type Variable struct {
	Name    string `json:"name"`
	Default string `json:"default"`
	// Required is true when the file refuses to start without a value
	// (the ${NAME:?} form).
	Required bool   `json:"required"`
	Kind     string `json:"kind"`
}

// referencedVariables lists the variables docker compose will interpolate in
// a file, sorted by name. Only values are scanned (compose does not
// interpolate keys or comments), and inline file contents are skipped: they
// are written to disk verbatim.
func (d document) referencedVariables() []Variable {
	found := map[string]*Variable{}
	collect(map[string]any(d), found)
	variables := make([]Variable, 0, len(found))
	for _, variable := range found {
		variables = append(variables, *variable)
	}
	sort.Slice(variables, func(i, j int) bool { return variables[i].Name < variables[j].Name })
	return variables
}

func collect(node any, found map[string]*Variable) {
	switch value := node.(type) {
	case string:
		scanReferences(value, found)
	case []any:
		for _, item := range value {
			collect(item, found)
		}
	case map[string]any:
		_, isMount := value["target"]
		for key, item := range value {
			if isMount && key == "content" {
				continue
			}
			collect(item, found)
		}
	}
}

func isNameStart(char byte) bool {
	return char == '_' || (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z')
}

func isNameChar(char byte) bool { return isNameStart(char) || (char >= '0' && char <= '9') }

// scanReferences finds $NAME and ${NAME…} in a string, following the compose
// interpolation syntax: "$$" is a literal dollar, and a default may itself
// reference other variables.
func scanReferences(text string, found map[string]*Variable) {
	for index := 0; index < len(text); index++ {
		if text[index] != '$' || index+1 >= len(text) {
			continue
		}
		next := text[index+1]
		switch {
		case next == '$':
			index++
		case next == '{':
			index = scanBraced(text, index+2, found)
		case isNameStart(next):
			end := index + 1
			for end < len(text) && isNameChar(text[end]) {
				end++
			}
			note(found, text[index+1:end], "", false)
			index = end - 1
		}
	}
}

// scanBraced reads what follows "${" and returns the index of the closing
// brace (or of the last byte, when the expression is not closed).
func scanBraced(text string, start int, found map[string]*Variable) int {
	end := start
	for end < len(text) && isNameChar(text[end]) {
		end++
	}
	name := text[start:end]
	closing := matchingBrace(text, end)
	if name == "" || closing < 0 {
		return len(text) - 1
	}
	operator, argument := splitOperator(text[end:closing])
	switch operator {
	case "-":
		note(found, name, argument, false)
		scanReferences(argument, found)
	case "?":
		note(found, name, "", true)
	default:
		note(found, name, "", false)
		scanReferences(argument, found)
	}
	return closing
}

// matchingBrace finds the brace that closes an expression opened before
// start, skipping nested ${…}.
func matchingBrace(text string, start int) int {
	depth := 0
	for index := start; index < len(text); index++ {
		switch {
		case text[index] == '$' && index+1 < len(text) && text[index+1] == '{':
			depth++
			index++
		case text[index] == '}':
			if depth == 0 {
				return index
			}
			depth--
		}
	}
	return -1
}

// splitOperator separates ":-default", "-default", ":?message", "?message",
// ":+alternative" and "+alternative" into operator and argument.
func splitOperator(expression string) (string, string) {
	expression = strings.TrimPrefix(expression, ":")
	if expression == "" {
		return "", ""
	}
	return expression[:1], expression[1:]
}

func note(found map[string]*Variable, name, fallback string, required bool) {
	variable, known := found[name]
	if !known {
		variable = &Variable{Name: name, Kind: variableKind(name)}
		found[name] = variable
	}
	if variable.Default == "" {
		variable.Default = fallback
	}
	variable.Required = variable.Required || required
}

func variableKind(name string) string {
	if _, generates := classify(name); generates || name == directoryVariable {
		return VariableGenerated
	}
	if hasAnyPrefix(name, "PIER_URL_", "PIER_HOST_", legacyPrefix+"URL_", legacyPrefix+"HOST_") {
		return VariableAddress
	}
	return VariableUser
}

func hasAnyPrefix(name string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
