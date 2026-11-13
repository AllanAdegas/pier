package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// sourceRoots are the folders whose messages reach people.
var sourceRoots = []string{"../../../internal", "../../../cmd"}

// accented and portugueseWord tell a Portuguese message from the English
// identifiers, commands and protocol strings that also live in the code.
var (
	accented       = regexp.MustCompile(`[ãõçáéíóúâêôàÃÕÇÁÉÍÓÚÂÊÔ]`)
	portugueseWord = regexp.MustCompile(`(?i)\b(para|uma?|em|com|ao|os|das?|dos?|que|sem|pel[oa]|nenhuma?|foi|ser|seu|sua|est[ea]|ou|ainda|mais|antes|depois|aqui|cada)\b`)
)

var hasLetters = regexp.MustCompile(`\pL`)

func looksPortuguese(text string) bool {
	if utf8.RuneCountInString(text) < 3 {
		return false
	}
	if accented.MatchString(text) {
		return true
	}
	return len(portugueseWord.FindAllString(text, -1)) >= 2
}

// sourceMessages collects every Portuguese string constant of the code, with
// where it is. Adjacent literals joined with + count as one message.
func sourceMessages(t *testing.T) map[string]string {
	t.Helper()
	messages := map[string]string{}
	files := token.NewFileSet()
	for _, root := range sourceRoots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(files, path, nil, 0)
			if err != nil {
				return err
			}
			var inspect func(ast.Node) bool
			inspect = func(node ast.Node) bool {
				if _, isImport := node.(*ast.ImportSpec); isImport {
					return false
				}
				if field, isField := node.(*ast.Field); isField && field.Tag != nil {
					return true
				}
				if pair, isPair := node.(*ast.KeyValueExpr); isPair {
					// A string key is data the code looks up (an accent to fold, a
					// category to rename), not a message; its value may be one.
					if _, isText := pair.Key.(*ast.BasicLit); isText {
						ast.Inspect(pair.Value, inspect)
						return false
					}
					if key, isField := pair.Key.(*ast.Ident); isField && messageFields[key.Name] {
						if text, isConstant := constantString(pair.Value); isConstant {
							messages[text] = files.Position(pair.Value.Pos()).String()
							return false
						}
					}
				}
				if call, isCall := node.(*ast.CallExpr); isCall {
					if argument := messageArgument(call); argument != nil {
						if text, isConstant := constantString(argument); isConstant && hasLetters.MatchString(verb.ReplaceAllString(text, "")) {
							messages[text] = files.Position(argument.Pos()).String()
						}
					}
					return true
				}
				text, isConstant := constantString(node)
				if !isConstant {
					return true
				}
				if looksPortuguese(text) {
					messages[text] = files.Position(node.Pos()).String()
				}
				return false
			}
			ast.Inspect(file, inspect)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return messages
}

// messageSinks are the functions whose argument at the given position is
// always a message for people, whatever it looks like.
var messageSinks = map[string]int{
	"apperr.Invalid": 0, "apperr.Conflict": 0, "apperr.Unavailable": 0, "apperr.Unauthorized": 0,
	"apperr.Forbidden": 0, "apperr.NotFound": 0, "fields.Add": 1, "i18n.Errorf": 0, "i18n.M": 0, "i18n.Text": 0,
	"log.Info": 0, "log.Error": 0, "log.Success": 0, "j.log.Info": 0, "j.log.Error": 0, "j.log.Success": 0,
	"s.runInBackground": 2,
}

// messageFields are struct fields that hold a message shown to people.
var messageFields = map[string]bool{"Description": true}

// translatingMethods translate their first argument whatever the receiver:
// a language (language.Text, i18n.Instance().Sprintf) or an operation log.
var translatingMethods = map[string]bool{"Text": true, "Sprintf": true, "Say": true}

func messageArgument(call *ast.CallExpr) ast.Expr {
	position, isSink := messageSinks[callName(call.Fun)]
	if selector, isMethod := call.Fun.(*ast.SelectorExpr); isMethod && translatingMethods[selector.Sel.Name] &&
		callName(selector.X) != "fmt" {
		position, isSink = 0, true
	}
	if !isSink || position >= len(call.Args) {
		return nil
	}
	return call.Args[position]
}

func callName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		if prefix := callName(value.X); prefix != "" {
			return prefix + "." + value.Sel.Name
		}
	}
	return ""
}

// constantString folds a string literal or a sum of string literals.
func constantString(node ast.Node) (string, bool) {
	switch value := node.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(value.Value)
		return text, err == nil
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return "", false
		}
		left, leftOK := constantString(value.X)
		right, rightOK := constantString(value.Y)
		return left + right, leftOK && rightOK
	case *ast.ParenExpr:
		return constantString(value.X)
	}
	return "", false
}

func TestEveryMessageHasAnEnglishTranslation(t *testing.T) {
	var missing []string
	for message, where := range sourceMessages(t) {
		if _, translated := english[message]; !translated && !untranslated[message] {
			missing = append(missing, where+"\n\t"+strconv.Quote(message)+`: "",`)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d messages without an English translation:\n%s", len(missing), strings.Join(missing, "\n"))
	}
}

func TestTheCatalogHasNoMessagesTheCodeStoppedUsing(t *testing.T) {
	used := sourceMessages(t)
	var stale []string
	for message := range english {
		if _, inUse := used[message]; !inUse {
			stale = append(stale, strconv.Quote(message))
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("%d translations of messages no longer in the code:\n%s", len(stale), strings.Join(stale, "\n"))
	}
}

var verb = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z%]`)

func TestTranslationsKeepTheFormatVerbs(t *testing.T) {
	for source, target := range english {
		if got, want := verb.FindAllString(target, -1), verb.FindAllString(source, -1); strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%q → %q: verbs %v, want %v", source, target, got, want)
		}
	}
}

// portugueseOnlyWord are words an English message would not contain, so one
// is enough to tell that a text that cannot be translated is in Portuguese.
var portugueseOnlyWord = regexp.MustCompile(`(?i)(?:^|[\s(])(não|muito|muita|informe|arquivo|senha|frase-senha|servidor|banco|inválid[oa]|encontrad[oa]|foi|para|com|sem|uma?|pel[oa]|ainda|nenhuma?|deve|pode|já|está|são|ser|seu|sua|ao|aos|dos|das|uso)(?:$|[\s,.:;)])`)

// untranslatableSinks build texts that never reach the catalog: fmt.Errorf
// and errors.New are printed as written, and so are the help texts of a flag
// set. People-facing text there must use i18n.Errorf or Language.Text.
var untranslatableSinks = map[string]int{
	"fmt.Errorf": 0, "errors.New": 0, "fmt.Println": 0, "fmt.Printf": 0, "fmt.Fprintf": 1, "fmt.Fprintln": 1,
	"flags.String": 2, "flags.Bool": 2, "flags.Int": 2, "flags.Duration": 2,
	"flag.String": 2, "flag.Bool": 2, "flag.Int": 2, "flag.Duration": 2,
}

func TestPortugueseNeverGoesThroughUntranslatableCalls(t *testing.T) {
	var found []string
	files := token.NewFileSet()
	for _, root := range sourceRoots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(files, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				position, isSink := untranslatableSinks[callName(call.Fun)]
				if !isSink || position >= len(call.Args) {
					return true
				}
				if text, isConstant := constantString(call.Args[position]); isConstant &&
					(accented.MatchString(text) || portugueseOnlyWord.MatchString(text)) {
					found = append(found, files.Position(call.Pos()).String()+"\n\t"+strconv.Quote(text))
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(found)
	if len(found) > 0 {
		t.Errorf("%d Portuguese texts in calls that never translate (use i18n.Errorf or Language.Text):\n%s",
			len(found), strings.Join(found, "\n"))
	}
}

func TestTheUntranslatableGuardTellsPortugueseFromEnglish(t *testing.T) {
	for _, text := range []string{"frase-senha muito longa", "informe --password-file", "uso: pier restore", "não foi possível"} {
		if !accented.MatchString(text) && !portugueseOnlyWord.MatchString(text) {
			t.Errorf("%q should read as Portuguese", text)
		}
	}
	for _, text := range []string{"create data dir: %w", "invalid PEM", "send e-mail via %s: %w", "PIER_URL (e.g. http://x.com) and PIER_TOKEN",
		"no enabled email channel", "set PIER_URL and PIER_TOKEN (a Pier API token)"} {
		if accented.MatchString(text) || portugueseOnlyWord.MatchString(text) {
			t.Errorf("%q should read as English", text)
		}
	}
}
