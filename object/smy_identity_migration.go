package object

import (
	"context"
	"fmt"

	"github.com/xorm-io/xorm"
	"github.com/xorm-io/xorm/schemas"
)

// MigrateSmyIdentityIndexes removes only the obsolete single-column mobile
// uniqueness constraint. The user and uniqueId constraints and all data stay.
// Run before Sync2 so startup does not depend on implicit index reconciliation.
func MigrateSmyIdentityIndexes(engine *xorm.Engine) error {
	table, err := engine.TableInfo(new(SmyMembershipCustomer))
	if err != nil {
		return err
	}
	found, err := engine.IsTableExist(new(SmyMembershipCustomer))
	if err != nil || !found {
		return err
	}
	indexes, err := engine.Dialect().GetIndexes(engine.DB(), context.Background(), table.Name)
	if err != nil {
		return err
	}
	for _, index := range indexes {
		if index.Type != schemas.UniqueType || len(index.Cols) != 1 || index.Cols[0] != "mobile" {
			continue
		}
		if _, err := engine.Exec(engine.Dialect().DropIndexSQL(table.Name, index)); err != nil {
			return fmt.Errorf("移除萨摩耶手机号唯一索引 %s 失败: %w", index.Name, err)
		}
	}
	return nil
}
