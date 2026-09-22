package main

import (
	"bytes"
	"fmt"
	"go/parser"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// config describes the project to the engine: where the domain model, the
// interface and the sqlc output live, the helpers and sentinels the adapters
// share, and the type conversions between the two sides. The engine reads
// nothing project-specific from anywhere else, so the same binary serves any
// repository laid out this way.
type config struct {
	Domain struct {
		Package       string `yaml:"package"`
		Import        string `yaml:"import"`
		Models        string `yaml:"models"`
		InterfaceFile string `yaml:"interface_file"`
		Interface     string `yaml:"interface"`
		ValueObjects  string `yaml:"value_objects"`
	} `yaml:"domain"`
	LockingDialect string            `yaml:"locking_dialect"`
	Dialects       []dialectSpec     `yaml:"dialects"`
	Conventions    conventions       `yaml:"conventions"`
	Types          []typeSpec        `yaml:"types"`
	Aliases        map[string]string `yaml:"aliases"`
	Deny           map[string]string `yaml:"deny"`
	Conversions    []rule            `yaml:"conversions"`
	Arguments      []rule            `yaml:"arguments"`
	Results        []rule            `yaml:"results"`
	Equivalents    []equivalent      `yaml:"equivalents"`

	types          []genSpec
	conv, arg, res map[[2]string]string
}

type dialectSpec struct {
	Name       string `yaml:"name"`
	Dir        string `yaml:"dir"`
	GenPackage string `yaml:"gen_package"`
	GenImport  string `yaml:"gen_import"`
	Adapter    string `yaml:"adapter"`
}

type typeSpec struct {
	Name   string `yaml:"name"`
	Domain string `yaml:"domain"`
	Row    string `yaml:"row"`
	Slice  bool   `yaml:"slice"`
	Plural string `yaml:"plural"`
}

// rule maps a source type to a target type through an expression template in
// which %s stands for the source value.
type rule struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
	Expr string `yaml:"expr"`
}

// equivalent names a helper whose call on &x is, by its definition, the
// literal with %s standing for x; normalization rewrites the call to the
// literal so both spellings compare equal.
type equivalent struct {
	Helper  string `yaml:"helper"`
	Literal string `yaml:"literal"`
}

// conventions are the identifiers generated bodies use, which every adapter
// package must define.
type conventions struct {
	QueriesField     string `yaml:"queries_field"`
	ErrorMapper      string `yaml:"error_mapper"`
	NotFound         string `yaml:"not_found"`
	TransactionCheck string `yaml:"transaction_check"`
	NoTransaction    string `yaml:"no_transaction"`
	Affected         string `yaml:"affected"`
	MapperSuffix     string `yaml:"mapper_suffix"`
	ErrorFormat      string `yaml:"error_format"`
}

func loadConfig(path string) (*config, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c config
	dec := yaml.NewDecoder(bytes.NewReader(src))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := c.resolve(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

// resolve validates the loaded values and builds the lookup tables.
func (c *config) resolve() error {
	cv := &c.Conventions
	for _, f := range []struct{ name, value string }{
		{"domain.package", c.Domain.Package}, {"domain.import", c.Domain.Import}, {"domain.models", c.Domain.Models},
		{"domain.interface_file", c.Domain.InterfaceFile}, {"domain.interface", c.Domain.Interface},
		{"domain.value_objects", c.Domain.ValueObjects}, {"locking_dialect", c.LockingDialect},
		{"conventions.queries_field", cv.QueriesField}, {"conventions.error_mapper", cv.ErrorMapper},
		{"conventions.not_found", cv.NotFound}, {"conventions.transaction_check", cv.TransactionCheck},
		{"conventions.no_transaction", cv.NoTransaction}, {"conventions.affected", cv.Affected},
		{"conventions.mapper_suffix", cv.MapperSuffix}, {"conventions.error_format", cv.ErrorFormat},
	} {
		if f.value == "" {
			return fmt.Errorf("%s is required", f.name)
		}
	}
	if !strings.Contains(cv.ErrorFormat, "%w") {
		return fmt.Errorf("conventions.error_format must wrap the error with %%w")
	}
	if len(c.Dialects) == 0 {
		return fmt.Errorf("at least one dialect is required")
	}
	dialects := map[string]bool{}
	for _, d := range c.Dialects {
		if d.Name == "" || d.Dir == "" || d.GenPackage == "" || d.GenImport == "" || d.Adapter == "" {
			return fmt.Errorf("dialect %q: name, dir, gen_package, gen_import and adapter are required", d.Name)
		}
		if dialects[d.Name] {
			return fmt.Errorf("dialect %q is listed twice", d.Name)
		}
		dialects[d.Name] = true
	}
	if !dialects[c.LockingDialect] {
		return fmt.Errorf("locking_dialect %q is not a dialect", c.LockingDialect)
	}
	types := map[string]bool{}
	for _, t := range c.Types {
		if t.Name == "" {
			return fmt.Errorf("types: name is required")
		}
		if types[t.Name] {
			return fmt.Errorf("type %q is listed twice", t.Name)
		}
		types[t.Name] = true
		c.types = append(c.types, genSpec{name: t.Name, domain: t.Domain, row: t.Row, slice: t.Slice, plural: t.Plural})
	}
	var err error
	if c.conv, err = ruleMap("conversions", c.Conversions); err != nil {
		return err
	}
	if c.arg, err = ruleMap("arguments", c.Arguments); err != nil {
		return err
	}
	if c.res, err = ruleMap("results", c.Results); err != nil {
		return err
	}
	for _, e := range c.Equivalents {
		if e.Helper == "" || strings.Count(e.Literal, "%s") != 1 {
			return fmt.Errorf("equivalents: %q needs a helper and a literal with one %%s", e.Helper)
		}
		if _, err := parser.ParseExpr(fmt.Sprintf(e.Literal, equivalentArg)); err != nil {
			return fmt.Errorf("equivalents: %s: %w", e.Helper, err)
		}
	}
	return nil
}

func ruleMap(section string, rules []rule) (map[[2]string]string, error) {
	out := make(map[[2]string]string, len(rules))
	for _, r := range rules {
		if r.From == "" || r.To == "" || !strings.Contains(r.Expr, "%s") {
			return nil, fmt.Errorf("%s: from, to and an expr containing %%s are required (%+v)", section, r)
		}
		key := [2]string{r.From, r.To}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("%s: %s -> %s is listed twice", section, r.From, r.To)
		}
		out[key] = r.Expr
	}
	return out, nil
}

// dialects builds the dialects with the row-locking set read from the
// locking dialect's sqlc output.
func (c *config) dialects(root string) ([]dialect, error) {
	var locks map[string]bool
	for _, d := range c.Dialects {
		if d.Name == c.LockingDialect {
			var err error
			if locks, err = rowLockQueries(filepath.Join(root, d.Dir, d.GenPackage)); err != nil {
				return nil, err
			}
		}
	}
	out := make([]dialect, len(c.Dialects))
	for i, d := range c.Dialects {
		out[i] = dialect{name: d.Name, dir: d.Dir, genPkg: d.GenPackage, genAlias: d.GenImport, adapterType: d.Adapter, rowLocks: locks}
	}
	return out, nil
}

// mapperSpec matches both types: several queries can project the same domain.
func (c *config) mapperSpec(domain, row string) (genSpec, bool) {
	for _, s := range c.types {
		if s.domainType() == domain && s.rowType() == row {
			return s, true
		}
	}
	return genSpec{}, false
}

// alias returns the sqlc query an interface method calls.
func (c *config) alias(name string) string {
	if q, ok := c.Aliases[name]; ok {
		return q
	}
	return name
}

func (c *config) denied(name string) bool {
	_, ok := c.Deny[name]
	return ok
}

// convertArg renders an interface argument of type from as the sqlc parameter
// type to, or reports that no rule exists.
func (c *config) convertArg(from, to, expr string) (string, bool) {
	if from == to {
		return expr, true
	}
	tmpl, ok := c.arg[[2]string{from, to}]
	if !ok {
		return "", false
	}
	return fmt.Sprintf(tmpl, expr), true
}

// conversion returns the template mapping a sqlc row field type to a domain
// field type.
func (c *config) conversion(from, to string) (string, bool) {
	tmpl, ok := c.conv[[2]string{from, to}]
	return tmpl, ok
}

// resultConv returns the template mapping a sqlc scalar result to the
// interface result type.
func (c *config) resultConv(from, to string) (string, bool) {
	tmpl, ok := c.res[[2]string{from, to}]
	return tmpl, ok
}
