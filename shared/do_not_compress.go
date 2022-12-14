package shared

var DO_NOT_COMPRESS = map[string]bool{
	".xz":   true,
	".gz":   true,
	".7z":   true,
	".zip":  true,
	".bz2":  true,
	".ogg":  true,
	".ogv":  true,
	".png":  true,
	".jpg":  true,
	".jp2":  true,
	".jpf":  true,
	".jpm":  true,
	".webp": true,
	".tiff": true,
	".mp3":  true,
	".amr":  true,
	".aac":  true,
	".mp4":  true,
	".m4a":  true,
	".m4v":  true,
	".webm": true,
	".mpeg": true,
	".mov":  true,
	".mqv":  true,
	".3gp":  true,
	".3g2":  true,
	".avi":  true,
	".flv":  true,
	".mkv":  true,
	".asf":  true,
	".deb":  true,
}

var DO_COMPRESS = map[string]bool{
	".txt":       true,
	".html":      true,
	"text/csv":   true,
	"text/plain": true,
	"text/html":  true,
	".svg":       true,
	".xml":       true,
	".php":       true,
	".js":        true,
	".lua":       true,
	".pl":        true,
	".py":        true,
	".json":      true,
	".geojson":   true,
	".har":       true,
	".ndjson":    true,
	".rtf":       true,
	".tcl":       true,
	".csv":       true,
	".tsv":       true,
	".vcf":       true,
	".ics":       true,
}
