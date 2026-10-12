// Package store define el layout en disco de versiones descargadas.
package store

import "path/filepath"

// Layout de una versión dentro de outBase/<id>/:
//
//	version.json          JSON detalle de la versión
//	client.jar            cliente (si side incluye client)
//	server.jar            servidor (si side incluye server)
//	client_mappings.txt   mappings oficiales (solo >= 1.14.4, opcional)
//	server_mappings.txt   idem
//	asset_index.json      índice de assets (NO los objetos; totalSize puede ser 650MB)
//	src-cfr/ src-vine/    salida de decompiladores
type Paths struct {
	Root           string // outBase/<id>
	VersionJSON    string
	ClientJar      string
	ServerJar      string
	ClientMappings string
	ServerMappings string
	AssetIndex     string
}

func ForVersion(outBase, versionID string) Paths {
	root := filepath.Join(outBase, versionID)
	return Paths{
		Root:           root,
		VersionJSON:    filepath.Join(root, "version.json"),
		ClientJar:      filepath.Join(root, "client.jar"),
		ServerJar:      filepath.Join(root, "server.jar"),
		ClientMappings: filepath.Join(root, "client_mappings.txt"),
		ServerMappings: filepath.Join(root, "server_mappings.txt"),
		AssetIndex:     filepath.Join(root, "asset_index.json"),
	}
}
