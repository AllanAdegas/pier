package secretbox

import (
	"errors"
	"fmt"
	"strings"
)

// Field is one database location that holds ciphertexts produced by Seal.
// The package knows nothing about which tables those are: the owners of the
// stores declare them (see internal/sealedfields) and pass them to
// UpgradeLegacy.
type Field struct {
	Table  string
	Column string
	// Row limits the field to the rows of a key/value table (such as
	// settings) whose Row.Column equals Row.Value. The zero value means
	// every row of the table.
	Row RowMatch
	// JSONPaths, when set, says the column holds a JSON document and lists
	// where its ciphertexts are: each path is a dot-separated chain of object
	// keys ending at a string value. Empty means the whole column value is
	// one ciphertext.
	JSONPaths []string
}

// RowMatch selects the rows of a key/value table by an exact (binary) match.
type RowMatch struct {
	Column string
	Value  string
}

// String names the field for errors and logs. It carries schema names
// only, never stored values.
func (f Field) String() string {
	name := f.Table + "." + f.Column
	if f.Row.Column != "" {
		name += "[" + f.Row.Column + "=" + f.Row.Value + "]"
	}
	if len(f.JSONPaths) > 0 {
		name += "/" + strings.Join(f.JSONPaths, ",")
	}
	return name
}

func (f Field) wholeValue() bool { return len(f.JSONPaths) == 0 }

func (f Field) validate() error {
	if f.Table == "" || f.Column == "" {
		return errors.New("secretbox: a field needs a table and a column")
	}
	if (f.Row.Column == "") != (f.Row.Value == "") {
		return fmt.Errorf("secretbox: field %s: a row match needs both a column and a value", f)
	}
	for _, path := range f.JSONPaths {
		for _, key := range strings.Split(path, ".") {
			if key == "" {
				return fmt.Errorf("secretbox: field %s: empty key in JSON path %q", f, path)
			}
		}
	}
	return nil
}

func validateFields(fields []Field) error {
	if len(fields) == 0 {
		return errors.New("secretbox upgrade: no encrypted fields registered")
	}
	for _, field := range fields {
		if err := field.validate(); err != nil {
			return err
		}
	}
	return nil
}
