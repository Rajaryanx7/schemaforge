package schemaforge

import (
	"fmt"
	"strings"
)

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func qualifiedTable(table string) string {
	return fmt.Sprintf("public.%s", quoteIdent(table))
}
