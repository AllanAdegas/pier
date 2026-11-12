package i18n

import "strings"

// Message is a text for people kept as its Portuguese format and arguments,
// so that it can be shown later in whichever language the reader uses.
type Message struct {
	Format string
	Args   []any
}

// M builds a message.
func M(format string, args ...any) Message { return Message{Format: format, Args: args} }

// String renders the message in Portuguese.
func (m Message) String() string { return m.Localize(Portuguese) }

// Localize renders the message in a language.
func (m Message) Localize(language Language) string { return language.Sprintf(m.Format, m.Args...) }

// Error is an error whose text is a Message: it reads in Portuguese through
// Error() and in any language through Localize. Errors given to a %w verb are
// kept for errors.Is and errors.As, as fmt.Errorf does.
type Error struct {
	Message
	wrapped []error
}

// Errorf is fmt.Errorf for messages people read.
func Errorf(format string, args ...any) error {
	err := &Error{Message: M(format, args...)}
	if strings.Contains(format, "%w") {
		for _, arg := range args {
			if wrapped, isError := arg.(error); isError {
				err.wrapped = append(err.wrapped, wrapped)
			}
		}
	}
	return err
}

func (e *Error) Error() string { return e.String() }

func (e *Error) Unwrap() []error { return e.wrapped }

// Localize renders an error in a language when it knows how, and otherwise
// as it is (errors from Docker, git or the network stay in English).
func Localize(err error, language Language) string {
	if localizer, ok := err.(Localizer); ok {
		return localizer.Localize(language)
	}
	return err.Error()
}

// formatVerbs adapts a format written for fmt.Errorf to fmt.Sprintf.
func formatVerbs(format string) string {
	if !strings.Contains(format, "%w") {
		return format
	}
	return strings.ReplaceAll(format, "%w", "%v")
}

// Join lists messages with a separator, each translated when shown.
func Join(messages []Message, separator string) Localizer {
	return joined{messages: messages, separator: separator}
}

type joined struct {
	messages  []Message
	separator string
}

func (j joined) Localize(language Language) string {
	parts := make([]string, len(j.messages))
	for i, message := range j.messages {
		parts[i] = message.Localize(language)
	}
	return strings.Join(parts, j.separator)
}

func (j joined) String() string { return j.Localize(Portuguese) }
