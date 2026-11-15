// Package settings keeps the user's choices between runs, as JSON in the
// user's configuration folder.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Settings are the choices Dalibre remembers.
type Settings struct {
	Library    string   `json:"library"`
	Recent     []string `json:"recent"`
	ListView   bool     `json:"listView"`
	HideDetail bool     `json:"hideDetail"`
	Sort       int      `json:"sort"`
	Descending bool     `json:"descending"`
	// EditorSidebar is the width of the book editor's sidebar the user
	// dragged it to; 0 is the default.
	EditorSidebar float32 `json:"editorSidebar,omitempty"`
	// Language is the language the user chose Dalibre to show, a tag of
	// its catalogs; "" follows the system's.
	Language string `json:"language,omitempty"`
	// Appearance is "light" or "dark" when the user chose one; "" follows
	// the system's.
	Appearance string `json:"appearance,omitempty"`
	// GetBooksSource is the source Get books searched last: a plugin id,
	// or "*" for all.
	GetBooksSource string `json:"getBooksSource,omitempty"`
	// Plugins are what the user decided about each plugin, by id.
	Plugins map[string]*PluginState `json:"plugins,omitempty"`
	// ToolSource is where the user last chose to get a plugin's tool from
	// ("download", "brew", …): the choice offered first next time.
	ToolSource string `json:"toolSource,omitempty"`
	// UpdatesChecked is when plugins installed from GitHub were last
	// checked for updates.
	UpdatesChecked time.Time `json:"updatesChecked,omitzero"`
	// OPDS is the catalogs the user added and sharing the library.
	OPDS OPDS `json:"opds,omitzero"`
	// AppUpdates is how Dalibre keeps itself up to date.
	AppUpdates AppUpdates `json:"appUpdates,omitzero"`
	// MCP is serving the library to the user's agents.
	MCP MCP `json:"mcp,omitzero"`
	// Wireless is letting readers connect as they connect to calibre,
	// to be sent books.
	Wireless Wireless `json:"wireless,omitzero"`

	path string
}

// AppUpdates is what the user chose about new versions of Dalibre.
type AppUpdates struct {
	// Manual turns the automatic checks off.
	Manual bool `json:"manual,omitempty"`
	// Beta offers prereleases too.
	Beta bool `json:"beta,omitempty"`
	// Checked is when the last check succeeded.
	Checked time.Time `json:"checked,omitzero"`
	// Skipped is the version the user chose to skip: automatic checks
	// don't offer it again.
	Skipped string `json:"skipped,omitempty"`
}

// OPDS is what the user set up of OPDS. Passwords aren't here: they are
// kept in the system's credential store.
type OPDS struct {
	Catalogs []*Catalog `json:"catalogs,omitempty"`
	// NotInGetBooks keeps catalogs out of Get books' sources.
	NotInGetBooks bool  `json:"notInGetBooks,omitempty"`
	Share         Share `json:"share,omitzero"`
}

// Catalog is an OPDS catalog the user added.
type Catalog struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	// LocalNetwork lets Dalibre reach this computer and the local network
	// for this catalog: it is a server at home.
	LocalNetwork bool `json:"localNetwork,omitempty"`
}

// Share is sharing the open library as an OPDS catalog on the network.
type Share struct {
	On       bool   `json:"on,omitempty"`
	Port     int    `json:"port,omitempty"` // DefaultSharePort when 0
	Username string `json:"username,omitempty"`
	// Password asks readers for the username and password.
	Password bool `json:"password,omitempty"`
}

// DefaultSharePort is where the library is shared unless the user picks
// another port.
const DefaultSharePort = 8080

// SharePort is the port the library is shared on.
func (s Share) SharePort() int {
	if s.Port <= 0 || s.Port > 65535 {
		return DefaultSharePort
	}
	return s.Port
}

// Catalog returns the catalog id, nil when there is none.
func (o *OPDS) Catalog(id string) *Catalog {
	for _, c := range o.Catalogs {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// MCP is serving the open library to the user's own agents over the
// Model Context Protocol, on this computer only.
type MCP struct {
	On   bool `json:"on,omitempty"`
	Port int  `json:"port,omitempty"` // DefaultMCPPort when 0
	// ReadOnly keeps agents from editing, adding and removing books; they
	// have every tool otherwise.
	ReadOnly bool `json:"readOnly,omitempty"`
	// Token is what agents send to be let in; made when the server first
	// starts. The stdio bridge reads it from here.
	Token string `json:"token,omitempty"`
}

// DefaultMCPPort is where the MCP server listens unless the user picks
// another port.
const DefaultMCPPort = 8765

// AllowsChanges reports whether agents may change the library.
func (m MCP) AllowsChanges() bool { return !m.ReadOnly }

// ServerPort is the port the MCP server listens on.
func (m MCP) ServerPort() int {
	if m.Port <= 0 || m.Port > 65535 {
		return DefaultMCPPort
	}
	return m.Port
}

// Wireless is calibre's wireless device connection, served for readers
// such as KOReader's calibre plugin.
type Wireless struct {
	On bool `json:"on,omitempty"`
	// Port is the port readers connect to; 0 is calibre's, 9090, or any
	// free one when it is taken.
	Port int `json:"port,omitempty"`
	// Password asks readers for the password, which is kept in the
	// system's credential store.
	Password bool `json:"password,omitempty"`
}

// PluginState is what the user decided about a plugin.
type PluginState struct {
	Disabled bool `json:"disabled,omitempty"`
	// Granted are the permissions the user granted; nil until they do.
	// A plugin asking for more than was granted isn't loaded until the
	// user grants it again.
	Granted *Grant            `json:"granted,omitempty"`
	Values  map[string]string `json:"values,omitempty"` // the plugin's settings
	// Answers are the last answers to its actions' forms that ask to be
	// remembered, by action id and field key.
	Answers map[string]map[string]any `json:"answers,omitempty"`
	// Source is where the plugin was installed from, when it can be
	// updated from there; nil for one installed from a file.
	Source *PluginSource `json:"source,omitempty"`
	// Path is the folder a plugin is linked from: it loads from there,
	// as the folder is, instead of from a copy in the plugins folder, and
	// reloads when the folder's files change. "" for an installed plugin.
	Path string `json:"path,omitempty"`
	// Tools are where each of the plugin's tools comes from, by tool id;
	// a tool missing here was never chosen.
	Tools map[string]*ToolChoice `json:"tools,omitempty"`
}

// ToolChoice is where a plugin's tool comes from: Source is "download"
// (Dalibre's own download of the pinned build), a package manager
// ("brew", "winget", "scoop"), or "file", with Path the file the user
// chose.
type ToolChoice struct {
	Source string `json:"source"`
	Path   string `json:"path,omitempty"`
}

// PluginSource is the GitHub repository a plugin came from, and what of
// it the plugin follows.
type PluginSource struct {
	Repo string `json:"repo"` // owner/name
	// Branch is the branch followed; "" follows releases.
	Branch string `json:"branch,omitempty"`
	// Ref is what is installed: a release's tag, or a commit.
	Ref string `json:"ref"`
	// Auto installs an update that asks for nothing new by itself.
	Auto bool `json:"auto,omitempty"`
	// Marketplace is true for a plugin installed from the Marketplace: its
	// updates are the versions the Marketplace lists, and Ref the version
	// installed.
	Marketplace bool `json:"marketplace,omitempty"`
}

// Grant is the permissions granted to a plugin, as its manifest has them.
type Grant struct {
	Hosts        []string `json:"hosts"`
	LocalNetwork bool     `json:"localNetwork,omitempty"`
	Library      string   `json:"library,omitempty"`
	// Covers is "any" when it may load covers from any website.
	Covers string `json:"covers,omitempty"`
	// Tools are the tools the user let it run, as plugin.Tool.Identity
	// names them, by id: a version and a build.
	Tools map[string]string `json:"tools,omitempty"`
}

// Plugin returns the state of plugin id, creating it.
func (s *Settings) Plugin(id string) *PluginState {
	if s.Plugins == nil {
		s.Plugins = map[string]*PluginState{}
	}
	if s.Plugins[id] == nil {
		s.Plugins[id] = &PluginState{}
	}
	return s.Plugins[id]
}

// DefaultPath is settings.json in the user's configuration folder.
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "Dalibre", "settings.json")
}

// Load reads the settings at path; missing or unreadable settings give
// the defaults. An empty path keeps the settings in memory only.
func Load(path string) *Settings {
	s := &Settings{Descending: true, path: path}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, s)
		}
	}
	return s
}

// Save writes the settings where they were loaded from.
func (s *Settings) Save() error {
	if s.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	// Only the user may read it: it holds the MCP token.
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Opened records root as the library in use, first among the recent ones.
func (s *Settings) Opened(root string) {
	s.Library = root
	s.Recent = append([]string{root}, slices.DeleteFunc(s.Recent, func(r string) bool { return r == root })...)
}
