package ethdb

//TODO database not SQL injection secure

import (

	"fmt"

	_ "github.com/lib/pq"
	"database/sql"
	"encoding/base64"
	"github.com/ethereum/go-ethereum/log"
	"strings"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/util"
	"strconv"
	"encoding/json"
)

const (
	host     = "35.200.194.52"
	port     = 5432
	user     = "postgres"
	password = "vvkaExD1rCerkG4F"
	dbname   = "psql_eth"
)

type PgSQLDatabase struct {
	db *sql.DB
	tableName string
} 

func NewPostgreSQLDb(tableName string) (*PgSQLDatabase, error) {
	//this removes '/', '-' from string
	tableName = strings.Replace(tableName,"/","",-1)
	tableName = strings.Replace(tableName,"-","",-1)
	tableName = strings.Replace(tableName,".","",-1)
	EnsureDatabaseExists()
	EnsureTableExists(tableName)

	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s "+
		"password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)
	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		return nil, err
	}

	return &PgSQLDatabase{
		db: db,
		tableName:tableName,
	},nil
}

//check if database exists, if not create it
func EnsureDatabaseExists(){
	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s "+
		"password=%s sslmode=disable",
		host, port, user, password)
	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		panic("could not get a connection:"+err.Error())
	}

	defer db.Close()

	err = db.Ping()
	if err != nil {
		panic("could not get a connection:"+err.Error())
	}

	//database exists if res.RowsAffected() returns 1, does not exists if returns 0
	res, err := db.Exec("SELECT 1 FROM pg_database WHERE datname = 'psql_eth';")
	if err != nil {
		panic(err)
	}
	exists,err := res.RowsAffected()
	if err != nil {
		panic(err)
	}
	if exists==0 {
		_, err := db.Exec("CREATE DATABASE psql_eth")
		if err != nil{
			panic(err)
		}
		log.Info("created db")
	}

}

//check if table exists, if not create it
func EnsureTableExists(tableName string){
	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s "+
		"password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)
	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		panic("Could not get a connection:"+err.Error())
	}

	defer db.Close()

	err = db.Ping()
	if err != nil {
		panic("could not get a connection:"+err.Error())
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS `+tableName+`(data jsonb)`)
	if err != nil {
		panic("Create table failed :"+err.Error())
	}
}

func (db *PgSQLDatabase) Put (key []byte, value []byte) error {
	keyBase64 := base64.StdEncoding.EncodeToString(key)
	valueBase64 := base64.StdEncoding.EncodeToString(value)
	hasKey, err := db.Has(key)
	if err!= nil {
		return err
	}
	if hasKey {
		sqlStatement := `UPDATE `+db.tableName+` SET data = $1
where data ->> $2 is not null;`
		_, err := db.db.Exec(sqlStatement,
			"{\""+keyBase64+"\":\""+valueBase64+"\"}", keyBase64)
		return err
	}else {
		sqlStatement := `INSERT INTO `+db.tableName+` VALUES ($1)`
		_, err := db.db.Exec(sqlStatement,
			"{\""+keyBase64+"\":\""+valueBase64+"\"}")
		return err
	}
}

func (db *PgSQLDatabase) Get (key []byte) ([]byte, error) {
	keyBase64 := base64.StdEncoding.EncodeToString(key)
	sqlStatement := `SELECT data->>$1 FROM `+db.tableName+`
WHERE data ->> $1 is not null;`
	var data string
	err := db.db.QueryRow(sqlStatement, keyBase64).Scan(&data)
	if err != nil {
		return nil, err
	}
	value, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (db *PgSQLDatabase) Has (key []byte) (bool, error){
	keyBase64 := base64.StdEncoding.EncodeToString(key)
	sqlStatement := `SELECT count(data->>$1) FROM `+db.tableName+`
WHERE data ->> $1 is not null;`
	var numRows int
	hasKey := false
	err := db.db.QueryRow(sqlStatement, keyBase64).Scan(&numRows)
	if numRows!=0{
		hasKey = true
	}
	return hasKey, err

}

func (db *PgSQLDatabase) Delete(key []byte) error{
	keyBase64 := base64.StdEncoding.EncodeToString(key)
	sqlStatement := `DELETE FROM `+db.tableName+` WHERE data ->> $1 is not null;`
	_, err := db.db.Exec(sqlStatement,keyBase64)
	return err
}

func (db *PgSQLDatabase) Close() {
	err := db.db.Close()
	if err != nil{
		panic(err)
	}
}

func (db *PgSQLDatabase) NewBatch() Batch {
	tx, err := db.db.Begin()
	if err != nil{
		panic(err)
	}
	return &psqlBatch{tx:tx}
}


type psqlBatch struct {
	db   *PgSQLDatabase
	tx   *sql.Tx
	size int
}

func (b *psqlBatch) Put(key []byte, value []byte) error  {
	keyBase64 := base64.StdEncoding.EncodeToString(key)
	valueBase64 := base64.StdEncoding.EncodeToString(value)
	hasKey, err := b.db.Has(key)
	if err!= nil {
		return err
	}
	if hasKey {
		sqlStatement := `UPDATE `+b.db.tableName+` SET data = $1
where data ->> $2 is not null;`
		_, err := b.tx.Exec(sqlStatement,
			"{\""+keyBase64+"\":\""+valueBase64+"\"}", keyBase64)
		b.size += len(value)
		return err
	}else {
		sqlStatement := `INSERT INTO `+b.db.tableName+` VALUES ($1)`
		_, err := b.tx.Exec(sqlStatement,
			"{\""+keyBase64+"\":\""+valueBase64+"\"}")
		b.size += len(value)
		return err
	}
}

func (b *psqlBatch) Write() error  {
	err := b.tx.Commit()
	return err
}

func (b *psqlBatch) ValueSize() int  {
	return b.size
}

func (db *PgSQLDatabase) NewIterator() iterator.Iterator {
	return &PgSQLIterator{
		offset:0,
		key:    make([]byte, 0),
		value:  make([]byte, 0),
		db: db,
	}
}

type PgSQLIterator struct {
	offset int
	db *PgSQLDatabase
	//CommonIterator
	key         []byte
	value       []byte
	err         error
}

//type CommonIterator struct {
//
//}
func (i *PgSQLIterator) Error() error {
	return i.err
}

func (i *PgSQLIterator) First() bool {
	var rowString string
	var jsonMap map[string]string
	sqlStatement := "SELECT * FROM " + i.db.tableName + " ORDER BY data ASC LIMIT 1 OFFSET 0"
	err := i.db.db.QueryRow(sqlStatement).Scan(&rowString)
	if err != nil{
		i.err = err
		return false
	}
	err = json.Unmarshal([]byte(rowString), &jsonMap)
	if err != nil {
		i.err = err
		return false
	}
	for key, value := range jsonMap {
		keyDecoded, err := base64.StdEncoding.DecodeString(key)
		if err!= nil{
			i.err = err
			return false
		}
		valueDecoded, err := base64.StdEncoding.DecodeString(value)
		if err!= nil{
			i.err = err
			return false
		}
		i.key = []byte(keyDecoded)
		i.value = []byte(valueDecoded)
	}
	i.offset=0
	return true
}

func (i *PgSQLIterator) Last() bool {
	var totalString string
	var totalInt int

	sqlStatementLast := "SELECT count(*) FROM " + i.db.tableName
	err := i.db.db.QueryRow(sqlStatementLast).Scan(&totalString)
	if err != nil{
		if err == sql.ErrNoRows{
			return false
		}
		i.err = err
		return false
	}

	totalInt, err = strconv.Atoi(totalString)
	if err != nil{
		i.err = err
		return false
	}

	var rowString string
	var jsonMap map[string]string
	sqlStatement2 := "SELECT * FROM " + i.db.tableName + " ORDER BY data ASC LIMIT 1 OFFSET " + strconv.Itoa(totalInt-1)
	err = i.db.db.QueryRow(sqlStatement2).Scan(&rowString)
	if err != nil{
		i.err = err
		return false
	}

	err = json.Unmarshal([]byte(rowString), &jsonMap)
	if err != nil {
		i.err = err
		return false
	}
	for key, value := range jsonMap {
		keyDecoded, err := base64.StdEncoding.DecodeString(key)
		if err!= nil{
			i.err = err
			return false
		}
		valueDecoded, err := base64.StdEncoding.DecodeString(value)
		if err!= nil{
			i.err = err
			return false
		}
		i.key = []byte(keyDecoded)
		i.value = []byte(valueDecoded)
	}

	i.offset,err = strconv.Atoi(strconv.Itoa(totalInt-1))
	if err != nil{
		i.err = err
		return false
	}
	return true
}

func (i *PgSQLIterator) Next() bool {
	var rowString string
	var jsonMap map[string]string

	sqlStatement := "SELECT * FROM " + i.db.tableName + " ORDER BY data ASC LIMIT 1 OFFSET " + strconv.Itoa(i.offset+1)
	err := i.db.db.QueryRow(sqlStatement).Scan(&rowString)
	if err != nil{
		i.err = err
		return false
	}
	err = json.Unmarshal([]byte(rowString), &jsonMap)
	if err != nil {
		i.err = err
		return false
	}
	for key, value := range jsonMap {
		keyDecoded, err := base64.StdEncoding.DecodeString(key)
		if err!= nil{
			i.err = err
			return false
		}
		valueDecoded, err := base64.StdEncoding.DecodeString(value)
		if err!= nil{
			i.err = err
			return false
		}
		i.key = []byte(keyDecoded)
		i.value = []byte(valueDecoded)
	}
	i.offset += 1
	return true
}

func (i *PgSQLIterator) Prev() bool {
	var rowString string
	var jsonMap map[string]string

	sqlStatement := "SELECT * FROM " + i.db.tableName + " ORDER BY data ASC LIMIT 1 OFFSET " + strconv.Itoa(i.offset-1)
	err := i.db.db.QueryRow(sqlStatement).Scan(&rowString)
	if err != nil{
		i.err = err
		return false
	}
	err = json.Unmarshal([]byte(rowString), &jsonMap)
	if err != nil {
		i.err = err
		return false
	}
	for key, value := range jsonMap {
		keyDecoded, err := base64.StdEncoding.DecodeString(key)
		if err!= nil{
			i.err = err
			return false
		}
		valueDecoded, err := base64.StdEncoding.DecodeString(value)
		if err!= nil{
			i.err = err
			return false
		}
		i.key = []byte(keyDecoded)
		i.value = []byte(valueDecoded)
	}
	i.offset -= 1
	return true
}

func (i *PgSQLIterator) Seek(key []byte) bool {
	i.First()

	var totalString string
	var totalInt int

	sqlStatementLast := "SELECT count(*) FROM " + i.db.tableName
	err := i.db.db.QueryRow(sqlStatementLast).Scan(&totalString)
	if err != nil{
		i.err = err
		return false
	}

	totalInt, err = strconv.Atoi(totalString)
	if err != nil{
		i.err = err
		return false
	}


	for j:=0; j<totalInt-1; j++{
		if string(i.key) >= string(key) {
			i.key = key
			i.offset = j
			return true
		}
		i.Next()
	}
	return true
}

func (i *PgSQLIterator) Key() []byte {
	return i.key
}

func (i *PgSQLIterator) Value() []byte {
	return i.value
}

func (i *PgSQLIterator) Release() {
	//doesn't do anything
}

func (i *PgSQLIterator) SetReleaser(releaser util.Releaser) {
	//doesn't do anything
}


func (i *PgSQLIterator) Valid() bool {
	if i.err != nil{
		return false
	}
	return true
}




