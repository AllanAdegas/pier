package database

import (
	"fmt"
	"net/url"
)

// Engine identifies a database product.
type Engine string

const (
	EnginePostgreSQL Engine = "postgresql"
	EngineMySQL      Engine = "mysql"
	EngineMariaDB    Engine = "mariadb"
	EngineMongoDB    Engine = "mongodb"
	EngineRedis      Engine = "redis"
	EngineKeyDB      Engine = "keydb"
	EngineDragonfly  Engine = "dragonfly"
	EngineClickHouse Engine = "clickhouse"
)

// Credentials are generated once, when the database is created, because the
// data volume is initialized with them.
type Credentials struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	Database     string `json:"database"`
	RootPassword string `json:"root_password"`
}

// dumpSpec describes how to take a logical backup from inside the container.
// The script reads credentials from the container environment, so no secret
// appears in a command line.
type dumpSpec struct {
	Script    string
	Extension string
	// Compress asks for gzip on the host after the dump is written.
	Compress bool
}

// engineSpec is everything that differs between engines. Adding an engine
// means adding one entry to the catalog (open/closed principle).
type engineSpec struct {
	Engine       Engine
	Label        string
	Description  string
	DefaultImage string
	Port         int
	DataPath     string
	Scheme       string
	// UsesAccount is false for engines that only have a password (Redis family).
	UsesAccount bool
	Environment func(c Credentials) map[string]string
	Command     func(c Credentials) []string
	Healthcheck string
	Dump        *dumpSpec
}

// EngineInfo is the public description of an engine, used by the catalog UI.
type EngineInfo struct {
	Engine         Engine `json:"engine"`
	Label          string `json:"label"`
	Description    string `json:"description"`
	DefaultImage   string `json:"default_image"`
	Port           int    `json:"port"`
	UsesAccount    bool   `json:"uses_account"`
	SupportsBackup bool   `json:"supports_backup"`
}

var catalog = []engineSpec{
	{
		Engine: EnginePostgreSQL, Label: "PostgreSQL", DefaultImage: "postgres:17-alpine", Port: 5432,
		Description: "Banco relacional robusto, a escolha padrão para a maioria das aplicações.",
		DataPath:    "/var/lib/postgresql/data", Scheme: "postgres", UsesAccount: true,
		Environment: func(c Credentials) map[string]string {
			return map[string]string{"POSTGRES_USER": c.Username, "POSTGRES_PASSWORD": c.Password, "POSTGRES_DB": c.Database}
		},
		Healthcheck: `pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"`,
		Dump: &dumpSpec{
			Script:    `PGPASSWORD="$POSTGRES_PASSWORD" pg_dump -U "$POSTGRES_USER" -Fc "$POSTGRES_DB"`,
			Extension: "dump",
		},
	},
	{
		Engine: EngineMySQL, Label: "MySQL", DefaultImage: "mysql:8.4", Port: 3306,
		Description: "Banco relacional popular, comum em aplicações PHP e legadas.",
		DataPath:    "/var/lib/mysql", Scheme: "mysql", UsesAccount: true,
		Environment: func(c Credentials) map[string]string {
			return map[string]string{
				"MYSQL_ROOT_PASSWORD": c.RootPassword, "MYSQL_USER": c.Username,
				"MYSQL_PASSWORD": c.Password, "MYSQL_DATABASE": c.Database,
			}
		},
		Healthcheck: `mysqladmin ping -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" --silent`,
		Dump: &dumpSpec{
			Script: `MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqldump -uroot --single-transaction --routines ` +
				`--no-tablespaces "$MYSQL_DATABASE"`,
			Extension: "sql", Compress: true,
		},
	},
	{
		Engine: EngineMariaDB, Label: "MariaDB", DefaultImage: "mariadb:11.4", Port: 3306,
		Description: "Fork comunitário do MySQL, compatível com o mesmo protocolo.",
		DataPath:    "/var/lib/mysql", Scheme: "mysql", UsesAccount: true,
		Environment: func(c Credentials) map[string]string {
			return map[string]string{
				"MARIADB_ROOT_PASSWORD": c.RootPassword, "MARIADB_USER": c.Username,
				"MARIADB_PASSWORD": c.Password, "MARIADB_DATABASE": c.Database,
			}
		},
		Healthcheck: `healthcheck.sh --connect --innodb_initialized`,
		Dump: &dumpSpec{
			Script: `MYSQL_PWD="$MARIADB_ROOT_PASSWORD" mariadb-dump -uroot --single-transaction --routines ` +
				`"$MARIADB_DATABASE"`,
			Extension: "sql", Compress: true,
		},
	},
	{
		Engine: EngineMongoDB, Label: "MongoDB", DefaultImage: "mongo:8.0", Port: 27017,
		Description: "Banco de documentos orientado a JSON.",
		DataPath:    "/data/db", Scheme: "mongodb", UsesAccount: true,
		Environment: func(c Credentials) map[string]string {
			return map[string]string{
				"MONGO_INITDB_ROOT_USERNAME": c.Username, "MONGO_INITDB_ROOT_PASSWORD": c.Password,
				"MONGO_INITDB_DATABASE": c.Database,
			}
		},
		Healthcheck: `mongosh --quiet --eval "db.adminCommand('ping').ok" | grep -q 1`,
		Dump: &dumpSpec{
			Script: `mongodump --quiet --archive --gzip --username "$MONGO_INITDB_ROOT_USERNAME" ` +
				`--password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin`,
			Extension: "archive.gz",
		},
	},
	{
		Engine: EngineRedis, Label: "Redis", DefaultImage: "redis:7.4-alpine", Port: 6379,
		Description: "Armazenamento chave-valor em memória para cache, filas e sessões.",
		DataPath:    "/data", Scheme: "redis",
		Command: func(c Credentials) []string {
			return []string{"redis-server", "--requirepass", c.Password, "--appendonly", "yes"}
		},
		Environment: func(c Credentials) map[string]string { return map[string]string{"REDISCLI_AUTH": c.Password} },
		Healthcheck: `redis-cli ping | grep -q PONG`,
	},
	{
		Engine: EngineKeyDB, Label: "KeyDB", DefaultImage: "eqalpha/keydb:latest", Port: 6379,
		Description: "Alternativa multithread compatível com o protocolo Redis.",
		DataPath:    "/data", Scheme: "redis",
		Command: func(c Credentials) []string {
			return []string{"keydb-server", "--requirepass", c.Password, "--appendonly", "yes"}
		},
		Environment: func(c Credentials) map[string]string { return map[string]string{"REDISCLI_AUTH": c.Password} },
		Healthcheck: `keydb-cli ping | grep -q PONG`,
	},
	{
		Engine: EngineDragonfly, Label: "Dragonfly", Port: 6379,
		DefaultImage: "docker.dragonflydb.io/dragonflydb/dragonfly:latest",
		Description:  "Armazenamento em memória de alto desempenho compatível com Redis.",
		DataPath:     "/data", Scheme: "redis",
		Command: func(c Credentials) []string {
			return []string{"dragonfly", "--requirepass", c.Password, "--dir", "/data"}
		},
		Environment: func(c Credentials) map[string]string { return map[string]string{"REDISCLI_AUTH": c.Password} },
		Healthcheck: `redis-cli ping | grep -q PONG`,
	},
	{
		Engine: EngineClickHouse, Label: "ClickHouse", DefaultImage: "clickhouse/clickhouse-server:latest",
		Port: 8123, Description: "Banco colunar para análises sobre grandes volumes de dados.",
		DataPath: "/var/lib/clickhouse", Scheme: "http", UsesAccount: true,
		Environment: func(c Credentials) map[string]string {
			return map[string]string{
				"CLICKHOUSE_USER": c.Username, "CLICKHOUSE_PASSWORD": c.Password, "CLICKHOUSE_DB": c.Database,
				"CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT": "1",
			}
		},
		Healthcheck: `wget -q -O /dev/null http://127.0.0.1:8123/ping`,
	},
}

func specFor(engine Engine) (engineSpec, bool) {
	for _, spec := range catalog {
		if spec.Engine == engine {
			return spec, true
		}
	}
	return engineSpec{}, false
}

// Engines lists the catalog for the UI.
func Engines() []EngineInfo {
	infos := make([]EngineInfo, len(catalog))
	for i, spec := range catalog {
		infos[i] = EngineInfo{
			Engine: spec.Engine, Label: spec.Label, Description: spec.Description, DefaultImage: spec.DefaultImage,
			Port: spec.Port, UsesAccount: spec.UsesAccount, SupportsBackup: spec.Dump != nil,
		}
	}
	return infos
}

// connectionURL builds the URL clients use to reach the database.
func (spec engineSpec) connectionURL(credentials Credentials, host string, port int) string {
	endpoint := url.URL{Scheme: spec.Scheme, Host: fmt.Sprintf("%s:%d", host, port)}
	switch {
	case !spec.UsesAccount:
		endpoint.User = url.UserPassword("default", credentials.Password)
		endpoint.Path = "/0"
	case spec.Engine == EngineMongoDB:
		endpoint.User = url.UserPassword(credentials.Username, credentials.Password)
		endpoint.Path = "/" + credentials.Database
		endpoint.RawQuery = "authSource=admin"
	case spec.Engine == EngineClickHouse:
		endpoint.User = url.UserPassword(credentials.Username, credentials.Password)
		endpoint.RawQuery = "database=" + url.QueryEscape(credentials.Database)
	default:
		endpoint.User = url.UserPassword(credentials.Username, credentials.Password)
		endpoint.Path = "/" + credentials.Database
	}
	return endpoint.String()
}
