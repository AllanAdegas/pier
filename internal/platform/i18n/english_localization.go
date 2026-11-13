package i18n

// englishLocalization holds what the 0.6 localization audit found printed or
// shown in Portuguese only: the help and errors of the pier command.
var englishLocalization = map[string]string{
	"uso: pier admin reset-password|disable-2fa --data-dir D --email E [--password-file F]": "usage: pier admin reset-password|disable-2fa --data-dir D --email E [--password-file F]",
	"uso: pier restore --file X --passphrase-file Y --data-dir D":                           "usage: pier restore --file X --passphrase-file Y --data-dir D",
	"E-mail da conta":               "Account email",
	"Arquivo com a nova senha":      "File with the new password",
	"informe --password-file":       "provide --password-file",
	"arquivo de senha muito grande": "password file too large",
	"Backup criptografado":          "Encrypted backup",
	"Arquivo com a frase-senha":     "File with the passphrase",
	"frase-senha muito longa":       "passphrase too long",
}

func init() { mergeEnglish(englishLocalization) }
