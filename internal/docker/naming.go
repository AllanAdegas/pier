package docker

import "strings"

const (
	shortIDLength = 6
	maxSlugLength = 30
)

// transliteration folds the accents of Portuguese and Spanish names, so
// "Produção" becomes "producao" instead of "produ-o".
var transliteration = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// ReadableName is a Docker name people can read in `docker ps`: the
// resource's name as a slug, then the start of its ID. The ID keeps names
// unique (two resources may share a name) and points back to the resource.
// Pier itself never finds containers by name, only by the pier.id label.
func ReadableName(name, id string) string {
	return Slug(name) + "-" + ShortID(id)
}

// ShortID is the start of an ID, enough to tell resources apart by eye.
func ShortID(id string) string {
	return id[:min(len(id), shortIDLength)]
}

// Slug turns a name into lowercase letters, digits and single dashes, which
// Docker accepts in container, project, network and volume names. A name
// with nothing usable becomes "pier".
func Slug(name string) string {
	folded := transliteration.Replace(strings.ToLower(strings.TrimSpace(name)))
	var slug strings.Builder
	dash := false
	for _, r := range folded {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			slug.WriteRune(r)
			dash = false
			continue
		}
		if !dash && slug.Len() > 0 {
			slug.WriteByte('-')
			dash = true
		}
	}
	result := strings.Trim(slug.String(), "-")
	if len(result) > maxSlugLength {
		result = strings.TrimRight(result[:maxSlugLength], "-")
	}
	if result == "" {
		return "pier"
	}
	return result
}
