package genutil

var ExternalMessages map[string]string

func init()  {
	ExternalMessages = make(map[string]string)
}

func GetPackageName(typename string) string {
	if pkg, ok := ExternalMessages[typename]; ok {
		return pkg
	} else {
		return "pb"
	}
}