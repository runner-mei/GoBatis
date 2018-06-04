package gobatis

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/runner-mei/GoBatis/reflectx"
)

const (
	DbTypeNone int = iota
	DbTypePostgres
	DbTypeMysql
	DbTypeMSSql
	DbTypeOracle
)

var dbNames = []string{
	"",
	"postgres",
	"mysql",
	"mssql",
	"oracle",
}

func ToDbType(driverName string) int {
	switch strings.ToLower(driverName) {
	case "postgres":
		return DbTypePostgres
	case "mysql":
		return DbTypeMysql
	case "mssql", "sqlserver":
		return DbTypeMSSql
	case "oracle", "ora":
		return DbTypeOracle
	default:
		return DbTypeNone
	}
}

func ToDbName(dbType int) string {
	if dbType >= 0 && dbType < len(dbNames) {
		return dbNames[dbType]
	}
	return "unknown"
}

type Connection struct {
	dbType        int
	db            dbRunner
	sqlStatements map[string]*MappedStatement
	mapper        *reflectx.Mapper
	isUnsafe      bool
}

func (sess *Connection) DbType() int {
	return sess.dbType
}

func (sess *Connection) Insert(id string, paramNames []string, paramValues []interface{}) (int64, error) {
	sqlStr, sqlParams, _, err := sess.readSQLParams(id, StatementTypeInsert, paramNames, paramValues)
	if err != nil {
		return 0, err
	}

	if ShowSQL {
		logger.Printf(`id:"%s", sql:"%s", params:"%+v"`, id, sqlStr, sqlParams)
	}

	if sess.dbType != DbTypePostgres && sess.dbType != DbTypeMSSql {
		result, err := sess.db.Exec(sqlStr, sqlParams...)
		if err != nil {
			return 0, err
		}
		return result.LastInsertId()
	}

	var insertID int64
	err = sess.db.QueryRow(sqlStr, sqlParams...).Scan(&insertID)
	if err != nil {
		return 0, err
	}
	return insertID, nil
}
func (sess *Connection) Update(id string, paramNames []string, paramValues []interface{}) (int64, error) {
	sqlStr, sqlParams, _, err := sess.readSQLParams(id, StatementTypeUpdate, paramNames, paramValues)
	if err != nil {
		return 0, err
	}

	if ShowSQL {
		logger.Printf(`id:"%s", sql:"%s", params:"%+v"`, id, sqlStr, sqlParams)
	}

	result, err := sess.db.Exec(sqlStr, sqlParams...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (sess *Connection) Delete(id string, paramNames []string, paramValues []interface{}) (int64, error) {
	sqlStr, sqlParams, _, err := sess.readSQLParams(id, StatementTypeDelete, paramNames, paramValues)
	if err != nil {
		return 0, err
	}

	if ShowSQL {
		logger.Printf(`id:"%s", sql:"%s", params:"%+v"`, id, sqlStr, sqlParams)
	}

	result, err := sess.db.Exec(sqlStr, sqlParams...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (sess *Connection) SelectOne(id string, paramNames []string, paramValues []interface{}) Result {
	sql, sqlParams, _, err := sess.readSQLParams(id, StatementTypeSelect, paramNames, paramValues)

	return Result{o: sess,
		id:        id,
		sql:       sql,
		sqlParams: sqlParams,
		err:       err}
}

func (sess *Connection) Select(id string, paramNames []string, paramValues []interface{}) *Results {
	sql, sqlParams, _, err := sess.readSQLParams(id, StatementTypeSelect, paramNames, paramValues)
	return &Results{o: sess,
		id:        id,
		sql:       sql,
		sqlParams: sqlParams,
		err:       err}
}

func (o *Connection) readSQLParams(id string, sqlType StatementType, paramNames []string, paramValues []interface{}) (sql string, sqlParams []interface{}, rType ResultType, err error) {
	sqlParams = make([]interface{}, 0)
	stmt, ok := o.sqlStatements[id]
	err = nil

	if !ok {
		err = fmt.Errorf("sql '%s' error : statement not found ", id)
		return
	}
	rType = stmt.result

	if stmt.sqlType != sqlType {
		err = fmt.Errorf("sql '%s' error : Select type Error, excepted is %s, actual is %s",
			id, sqlType.String(), stmt.sqlType.String())
		return
	}

	if stmt.sqlTemplate == nil {
		if stmt.sqlCompiled == nil {
			sql = stmt.sql
			if len(paramNames) != 0 {
				sqlParams = paramValues
			}
			return
		}

		if bindType := BindType(o.dbType); bindType == QUESTION {
			sql = stmt.sqlCompiled.questSQL
		} else {
			sql = stmt.sqlCompiled.dollarSQL
		}
		sqlParams, err = bindNamedQuery(stmt.sqlCompiled.bindNames, paramNames, paramValues, o.mapper)
		if err != nil {
			err = fmt.Errorf("sql '%s' error : %s", id, err)
		}
		return
	}

	var tplArgs interface{}
	if len(paramNames) == 0 {
		if len(paramValues) == 0 {
			err = fmt.Errorf("sql '%s' error : arguments is missing", id)
			return
		}
		if len(paramValues) > 1 {
			err = fmt.Errorf("sql '%s' error : arguments is exceed 1", id)
			return
		}

		tplArgs = paramValues[0]
	} else if len(paramNames) == 1 {
		tplArgs = paramValues[0]
		if _, ok := tplArgs.(map[string]interface{}); !ok {
			paramType := reflect.TypeOf(tplArgs)
			if paramType.Kind() == reflect.Ptr {
				paramType = paramType.Elem()
			}
			if paramType.Kind() != reflect.Struct {
				tplArgs = map[string]interface{}{paramNames[0]: paramValues[0]}
			}
		}
	} else {
		var args = map[string]interface{}{}
		for idx := range paramNames {
			args[paramNames[idx]] = paramValues[idx]
		}
		tplArgs = args
	}

	var sb strings.Builder
	err = stmt.sqlTemplate.Execute(&sb, tplArgs)
	if err != nil {
		err = fmt.Errorf("1sql '%s' error : %s", id, err)
		return
	}
	sql = sb.String()

	fragments, nameArgs, e := compileNamedQuery(sql)
	if e != nil {
		err = fmt.Errorf("2sql '%s' error : %s", id, e)
		return
	}
	if len(nameArgs) == 0 {
		return
	}

	sql = concatFragments(BindType(o.dbType), fragments, nameArgs)
	sqlParams, err = bindNamedQuery(nameArgs, paramNames, paramValues, o.mapper)
	if err != nil {
		err = fmt.Errorf("3sql '%s' error : %s", id, err)
	}
	return
}
