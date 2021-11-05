package mime

type MimeType string

const (
	ApplicationJson MimeType = "application/json"
	ApplicationXml  MimeType = "application/xml"
	ApplicationYaml MimeType = "application/yaml"
	TextPlain       MimeType = "text/plain"
)

func GuessMarkup(config string) MimeType {
	switch config[0:1] {
	case "<":
		return ApplicationXml
	case "{":
		return ApplicationJson
	default:
		return ApplicationYaml
	}
}
