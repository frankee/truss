package templates

const HandlerMethods = `
{{ with $te := .}}
		{{range $i := .Methods}}
		// {{.Name}} implements Service.
		func (s {{ToLowCamelName $te.ServiceName}}Service) {{.Name}}(ctx context.Context, in *{{PackageName $i.RequestType.Name}}.{{GoName .RequestType.Name}}) (*{{PackageName $i.ResponseType.Name}}.{{GoName .ResponseType.Name}}, error){
			var resp {{PackageName $i.ResponseType.Name}}.{{GoName .ResponseType.Name}}
			resp = {{PackageName $i.ResponseType.Name}}.{{GoName .ResponseType.Name}}{
				{{range $j := $i.ResponseType.Message.Fields -}}
					// {{GoName $j.Name}}:
				{{end -}}
			}
			return &resp, nil
		}
		{{end}}
{{- end}}
`

const Handlers = `
package handlers

import (
	"context"
	"errors"
	
	{{range $i := .ExternalMessageImports}}
	"{{$i}}"
	{{- end}}
	pb "{{.PBImportPath -}}"
)

// NewService returns a naïve, stateless implementation of Service.
func NewService() pb.{{GoName .Service.Name}}Server {
	return {{ToLowCamelName .Service.Name}}Service{}
}

type {{ToLowCamelName .Service.Name}}Service struct{}

{{with $te := . }}
	{{range $i := $te.Service.Methods}}
		// {{$i.Name}} implements Service.
		func (s {{ToLowCamelName $te.Service.Name}}Service) {{$i.Name}}(ctx context.Context, in *{{PackageName $i.RequestType.Name}}.{{GoName $i.RequestType.Name}}) (*{{PackageName $i.ResponseType.Name}}.{{GoName $i.ResponseType.Name}}, error){
			var resp {{PackageName $i.ResponseType.Name}}.{{GoName $i.ResponseType.Name}}
			resp = {{PackageName $i.ResponseType.Name}}.{{GoName $i.ResponseType.Name}}{
				{{range $j := $i.ResponseType.Message.Fields -}}
					// {{GoName $j.Name}}:
				{{end -}}
			}
			return &resp, nil
		}
	{{end}}
{{- end}}
`
