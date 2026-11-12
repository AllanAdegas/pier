package i18n

var englishAuth = map[string]string{
	"Limite de requisições do token excedido. Tente de novo em %d s.": "Token request limit exceeded. Try again in %d s.",
	"Pier — redefinir senha": "Pier — reset password",
	"Para redefinir sua senha, abra o link em até 30 minutos:\n%s\nSe você não pediu, ignore esta mensagem.": "To reset your password, open this link within 30 minutes:\n%s\nIf you did not request this, ignore this message.",
	"informe ação, --data-dir e --email":                                                             "provide an action, --data-dir and --email",
	"Muitas verificações em andamento. Tente de novo em alguns minutos.":                             "Too many verifications in progress. Try again in a few minutes.",
	"A verificação expirou. Entre com a senha de novo.":                                              "Verification expired. Sign in with your password again.",
	"O link de redefinição é inválido ou expirou. Peça um novo.":                                     "The reset link is invalid or expired. Request a new one.",
	"A recuperação por e-mail não está configurada. Peça a um administrador para redefinir a senha.": "Email recovery is not configured. Ask an administrator to reset your password.",
	"Sessão":                  "Session",
	"A senha está incorreta.": "The password is incorrect.",
	"A verificação em duas etapas não está ativa.":                                "Two-step verification is not enabled.",
	"A verificação em duas etapas não está disponível nesta instância.":           "Two-step verification is not available on this instance.",
	"Código incorreto ou já usado. Confira o horário do celular e tente de novo.": "Incorrect or already used code. Check your phone clock and try again.",
	"A verificação em duas etapas já está ativa.":                                 "Two-step verification is already enabled.",
	"Nenhuma ativação em andamento. Comece de novo.":                              "No enrollment in progress. Start again.",
}

func init() { mergeEnglish(englishAuth) }
