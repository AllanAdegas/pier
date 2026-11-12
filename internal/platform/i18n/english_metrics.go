package i18n

var englishMetrics = map[string]string{
	"Intervalo inválido. Use 1h, 6h, 24h, 7d ou 30d.": "Invalid range. Use 1h, 6h, 24h, 7d or 30d.",
	"O histórico de métricas não está disponível.":    "Metrics history is not available.",
	"Tipo de recurso": "Resource type",
}

func init() { mergeEnglish(englishMetrics) }
