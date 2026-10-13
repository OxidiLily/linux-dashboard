package helperproto

const CmdPostfixRead = "postfix.read"
const CmdPostfixSave = "postfix.save"

type PostfixRoute struct {
	Domain string `json:"domain"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
}

type PostfixConfig struct {
	Routes          []PostfixRoute    `json:"routes"`
	Values          map[string]string `json:"values"`
	Revision        string            `json:"revision"`
	TransportExists bool              `json:"transport_exists"`
	Restart         bool              `json:"restart"`
}
