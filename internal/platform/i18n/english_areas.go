package i18n

// mergeEnglish adds an area's translations to the catalog. Each area keeps
// its own file (english_<area>.go) so parallel work does not collide on one map.
func mergeEnglish(area map[string]string) {
	for source, target := range area {
		english[source] = target
	}
}
