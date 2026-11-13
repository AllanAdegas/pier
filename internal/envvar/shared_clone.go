package envvar

import "regexp"

// referenceOnlyPattern matches a value made only of references, which holds
// no secret of its own and resolves against the destination.
var referenceOnlyPattern = regexp.MustCompile(`^(\$\{\{[^{}]+\.[A-Za-z0-9_]+\}\})+$`)

// ForClone prepares variables copied into another resource. With keepValues
// every value is copied. Without it (a copy into another project that was not
// explicitly allowed to receive secrets) only the keys and flags are copied;
// values are left empty, except values made only of references.
func ForClone(variables []Variable, keepValues bool) []Variable {
	copies := make([]Variable, 0, len(variables))
	for _, variable := range variables {
		copied := Variable{Key: variable.Key, Value: variable.Value, BuildTime: variable.BuildTime, SecretBuild: variable.SecretBuild}
		if !keepValues && !referenceOnlyPattern.MatchString(variable.Value) {
			copied.Value = ""
		}
		copies = append(copies, copied)
	}
	return copies
}
