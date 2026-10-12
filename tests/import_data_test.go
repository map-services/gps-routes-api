//go:build !sqlite

package tests

import (
	"testing"

	"github.com/map-services/gps-routes-api/cmds"
)

func TestImportData(t *testing.T) {
	cmds.ImportData("../data/backup", 5)
}
