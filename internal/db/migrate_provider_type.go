package db

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
	"jenken827.com/jenken827/pfm/pkg/llog"
)

// legacyProviderTypeNames 旧 StorageProviderType 枚举值到驱动身份的映射。
//
// 枚举已删除（驱动身份改为字符串 driver_id），这里保留一次性迁移所需的映射，
// 迁移在所有部署上执行完之后可以整段删除。
var legacyProviderTypeNames = map[int64]string{
	1: "local",
	2: "telegram",
	3: "discord",
	4: "huggingface",
	5: "pan123",
	6: "baidu",
	7: "aliyundrive_open",
	8: "yun139",
	9: "cloud189pc",
}

// migrateProviderTypeToString 把 storage_providers.provider_type 从旧枚举
// （整型）迁移成驱动身份（varchar(32)）。
//
// 必须跑在 AutoMigrate 之前：模型已经是 string，GORM 对 int -> varchar 只会生成
// 没有 USING 的 ALTER COLUMN，Postgres 会直接报错。幂等：列已是字符串/表不存在时
// 直接返回。各方言都在事务里做，中途失败回滚后可以重跑。
func migrateProviderTypeToString(gdb *gorm.DB) error {
	columnType, exists, err := providerTypeColumnType(gdb)
	if err != nil {
		return err
	}
	if !exists || isStringColumnType(columnType) {
		return nil
	}
	llog.L().Infof("migrating storage_providers.provider_type from %s to varchar(32)", columnType)
	if err := gdb.Transaction(func(tx *gorm.DB) error {
		switch tx.Name() {
		case "postgres":
			return migrateProviderTypePostgres(tx)
		case "mysql":
			return migrateProviderTypeMySQL(tx)
		default:
			return migrateProviderTypeSQLite(tx)
		}
	}); err != nil {
		return fmt.Errorf("migrate storage_providers.provider_type failed: %w", err)
	}
	return warnUnknownProviderTypes(gdb)
}

// providerTypeColumnType 返回 provider_type 的列类型（表或列不存在时 exists=false）
func providerTypeColumnType(gdb *gorm.DB) (string, bool, error) {
	var columnType string
	var err error
	switch gdb.Name() {
	case "postgres":
		err = gdb.Raw(`SELECT data_type FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = 'storage_providers'
			AND column_name = 'provider_type'`).Row().Scan(&columnType)
	case "mysql":
		err = gdb.Raw(`SELECT data_type FROM information_schema.columns
			WHERE table_schema = DATABASE() AND table_name = 'storage_providers'
			AND column_name = 'provider_type'`).Row().Scan(&columnType)
	case "sqlite", "sqlite3":
		err = gdb.Raw(`SELECT type FROM pragma_table_info('storage_providers') WHERE name = 'provider_type'`).
			Row().Scan(&columnType)
	default:
		return "", false, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query provider_type column type failed: %w", err)
	}
	return columnType, true, nil
}

// isStringColumnType 判断列类型是否已经是字符串
func isStringColumnType(columnType string) bool {
	t := strings.ToLower(columnType)
	return strings.Contains(t, "char") || strings.Contains(t, "text")
}

// providerTypeCaseSQL 生成枚举值 -> 驱动身份的 CASE 表达式（整型列直接比较）。
//
// unknownExpr 是未知值的落库表达式，各方言的字符串拼接写法不同：
// Postgres 要先把整型转文本（"::text"），SQLite 直接用 "||"，而 MySQL 的 "||"
// 默认是逻辑或，只能用 CONCAT。
func providerTypeCaseSQL(unknownExpr string) string {
	values := make([]int64, 0, len(legacyProviderTypeNames))
	for value := range legacyProviderTypeNames {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })

	var b strings.Builder
	b.WriteString("CASE provider_type")
	for _, value := range values {
		fmt.Fprintf(&b, " WHEN %d THEN '%s'", value, legacyProviderTypeNames[value])
	}
	fmt.Fprintf(&b, " ELSE %s END", unknownExpr)
	return b.String()
}

// migrateProviderTypePostgres 单条 ALTER 完成类型转换与取值映射
func migrateProviderTypePostgres(tx *gorm.DB) error {
	sql := fmt.Sprintf(
		"ALTER TABLE storage_providers ALTER COLUMN provider_type TYPE varchar(32) USING (%s)",
		providerTypeCaseSQL("'unknown_' || provider_type::text"))
	return tx.Exec(sql).Error
}

// migrateProviderTypeMySQL 不支持在 ALTER 里做取值映射：先把整型列改成
// varchar（数值原样转成字符串），再用 UPDATE 映射成驱动身份。
func migrateProviderTypeMySQL(tx *gorm.DB) error {
	steps := []string{
		"ALTER TABLE storage_providers MODIFY COLUMN provider_type varchar(32) NOT NULL DEFAULT ''",
		"UPDATE storage_providers SET provider_type = " +
			providerTypeCaseSQL("CONCAT('unknown_', provider_type)"),
	}
	for _, sql := range steps {
		if err := tx.Exec(sql).Error; err != nil {
			return fmt.Errorf("%s failed: %w", sql, err)
		}
	}
	return nil
}

// migrateProviderTypeSQLite 不支持 ALTER COLUMN，走「加列 - 回填 - 删列 - 改名」
func migrateProviderTypeSQLite(tx *gorm.DB) error {
	steps := []string{
		"ALTER TABLE storage_providers ADD COLUMN provider_type_new varchar(32) NOT NULL DEFAULT ''",
		"UPDATE storage_providers SET provider_type_new = " + providerTypeCaseSQL("'unknown_' || provider_type"),
		"ALTER TABLE storage_providers DROP COLUMN provider_type",
		"ALTER TABLE storage_providers RENAME COLUMN provider_type_new TO provider_type",
	}
	for _, sql := range steps {
		if err := tx.Exec(sql).Error; err != nil {
			return fmt.Errorf("%s failed: %w", sql, err)
		}
	}
	return nil
}

// warnUnknownProviderTypes 迁移后统计无法识别的旧值（0 是未指定、其余是未知枚举）
func warnUnknownProviderTypes(gdb *gorm.DB) error {
	rows, err := gdb.Raw(`SELECT id, provider_type FROM storage_providers WHERE provider_type LIKE 'unknown_%'`).Rows()
	if err != nil {
		return fmt.Errorf("query unknown provider_type failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id uint32
		var value string
		if scanErr := rows.Scan(&id, &value); scanErr != nil {
			return fmt.Errorf("scan unknown provider_type failed: %w", scanErr)
		}
		ids = append(ids, fmt.Sprintf("%d(%s)", id, value))
	}
	if len(ids) > 0 {
		llog.L().Warnf("storage_providers with unrecognized provider_type after migration: %s", strings.Join(ids, ", "))
	}
	llog.L().Info("migrated storage_providers.provider_type to driver_id strings")
	return nil
}
