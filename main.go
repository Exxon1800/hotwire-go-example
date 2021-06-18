package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"github.com/while1malloc0/hotwire-go-example/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"html/template"
	"io"
	"net/http"
	"strconv"
)

type Page struct {
	Title string
	Body  string
}

type PaginateDataStruct struct {
	Draw            string              `json:"draw"`
	RecordsTotal    int                 `json:"recordsTotal"`
	RecordsFiltered int                 `json:"recordsFiltered"`
	Data            []map[string]string `json:"data"`
}

type dataTablesField struct {
	dtName       string
	databaseName string
}

var db *gorm.DB

func main() {
	log.Println("Starting app")

	db = setupDB()

	r := mux.NewRouter()
	r.HandleFunc("/", indexHandler).Methods("GET")
	r.HandleFunc("/populateDataTable", pagingHandler).Methods("POST")
	http.Handle("/", r)
	_ = http.ListenAndServe(":8080", nil)
}

// search function returns the result of the query
func search(sortingField dataTablesField, dtFields []dataTablesField, tableName string, args []interface{}) (dataList []map[string]string, err error) {
	var (
		rows                *sql.Rows
		noSearchValueInArgs = 2
		searchValueInArgs   = 3
	)

	switch len(args) {
	case searchValueInArgs:
		limit, offset, err := getLimitAndOffset(args)
		if err != nil {
			return nil, err
		}

		rows, err = db.
			Table(tableName).
			Select(sortingField.databaseName).
			Where(fmt.Sprintf("%s LIKE ?", sortingField.databaseName), args...).
			Order(sortingField.databaseName).
			Offset(offset).
			Limit(limit).
			Rows()
		if err != nil {
			return nil, fmt.Errorf("could not execute filtered search query %w", err)
		}
	case noSearchValueInArgs:
		limit, offset, err := getLimitAndOffset(args)
		if err != nil {
			return nil, err
		}

		rows, err = db.Debug().
			Table(tableName).
			Select(sortingField.databaseName).
			Order(sortingField.databaseName).
			Offset(offset).
			Limit(limit).
			Rows()
		if err != nil {
			return nil, fmt.Errorf("could not execute search query %w", err)
		}
	default:
		return nil, fmt.Errorf("incorrect argument list size for args %v", args)
	}

	defer func() {
		_ = rows.Close()

		err = rows.Err()
	}()

	if err != nil {
		return nil, fmt.Errorf("row error occurred %w", err)
	}

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("could not get row.Columns %w", err)
	}

	values := make([]sql.RawBytes, len(columns))
	// rows.Scan wants '[]interface{}' as an argument, so we must copy the
	// references into such a slice
	// See http://code.google.com/p/go-wiki/wiki/InterfaceSlice for details
	scanArgs := make([]interface{}, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	for rows.Next() {
		// get RawBytes from data
		err = rows.Scan(scanArgs...)
		if err != nil {
			panic(err)
		}

		var value string

		for i, col := range values {
			// Here we can check if the value is nil (NULL value)
			if col == nil {
				value = "NULL"
			} else {
				value = string(col)
			}

			for _, dtField := range dtFields {
				if dtField.databaseName == columns[i] {
					dtObject := map[string]string{dtField.dtName: value}
					dataList = append(dataList, dtObject)
				}
			}
		}
	}

	return dataList, nil
}

func getLimitAndOffset(args []interface{}) (int, int, error) {
	limit, err := strconv.Atoi(fmt.Sprintf("%v", args[len(args)-1]))
	if err != nil {
		return 0, 0, fmt.Errorf("could not convert limit string to int %w", err)
	}

	offset, err := strconv.Atoi(fmt.Sprintf("%v", args[len(args)-2]))
	if err != nil {
		return 0, 0, fmt.Errorf("could not convert offset string to int %w", err)
	}

	return limit, offset, nil
}

var final int

func pagingHandler(w http.ResponseWriter, r *http.Request, ) {
	err := paging(w, r, "rooms", []dataTablesField{{"name", "name"}})
	if err != nil {
		panic(err)
	}
}

func paging(w http.ResponseWriter, r *http.Request, tableName string, dtFields []dataTablesField) error {
	var (
		pagingData PaginateDataStruct
		result     []map[string]string
		args       []interface{}
		firstDraw  = "1"
	)

	err := r.ParseForm()
	if err != nil {
		return fmt.Errorf("could not parse form %w", err)
	}

	start := r.FormValue("start")
	end := r.FormValue("length")
	draw := r.FormValue("draw")
	searchValue := r.FormValue("search[value]")

	if draw == firstDraw {
		rows, err := db.Table(tableName).Select("COUNT(*)").Rows()
		if err != nil {
			return fmt.Errorf("could not execute query to get the rowcount of the entire '%v' table %w", tableName, err)
		}

		defer func() {
			_ = rows.Close()

			err = rows.Err()
		}()

		if err != nil {
			return fmt.Errorf("row error occurred %w", err)
		}

		for rows.Next() {
			err = rows.Scan(&final)
			if err != nil {
				return fmt.Errorf("could not scan row to &final %w", err)
			}
		}
	}

	if searchValue != "" {
		p := searchValue + "%"
		args = []interface{}{p, start, end}
		aux := []interface{}{p}

		result, err = search(dtFields[0], dtFields, tableName, args)
		if err != nil {
			return err
		}

		// Here we obtain the number of results
		rows, err := db.Debug().
			Model(&models.Room{}).
			Select("COUNT(*)").
			Where(generateDTWhereQuery(dtFields), aux...).
			Order(dtFields[0].databaseName).
			Rows()
		if err != nil {
			return fmt.Errorf("could not execute query to get the number of results %w", err)
		}

		defer func() {
			_ = rows.Close()

			err = rows.Err()
		}()

		if err != nil {
			return fmt.Errorf("row error occurred %w", err)
		}

		for rows.Next() {
			err = rows.Scan(&final)
			if err != nil {
				return fmt.Errorf("could not scan row to &final %w", err)
			}
		}
	} else {
		args = []interface{}{start, end}
		result, err = search(dtFields[0], dtFields, tableName, args)
		if err != nil {
			return err
		}
	}

	pagingData.Data = result
	pagingData.Draw = draw
	pagingData.RecordsFiltered = final

	jsonData, err := json.Marshal(pagingData)
	if err != nil {
		return fmt.Errorf("could not marshal pagingData %w", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_, err = w.Write(jsonData)
	if err != nil {
		return fmt.Errorf("could not write json data to the connection %w", err)
	}

	return nil
}

func generateDTWhereQuery(dtFields []dataTablesField) string {
	whereQuery := fmt.Sprintf("%s like ? ", dtFields[0].databaseName)

	for _, field := range dtFields[1:] {
		whereQuery += fmt.Sprintf("OR %s like ? ", field.databaseName)
	}

	return whereQuery
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	p := Page{
		Title: "pagination",
	}

	t, err := template.ParseFiles("templates/index.html")
	ifErrorToPage(w, r, err)

	err = t.Execute(w, p)
	ifErrorToPage(w, r, err)
}

// TODO only for this test setup
func ifErrorToPage(w io.Writer, r *http.Request, err error) {
	if err != nil {
		t, err := template.ParseFiles("templates/Error.html")
		if logIfError(r, err) {
			return
		}

		err = t.Execute(w, err)
		if logIfError(r, err) {
			return
		}
	}
}

func logIfError(r *http.Request, err error) bool {
	if err != nil {
		logError(r, err)

		return true
	}

	return false
}

func logError(r *http.Request, err error) *log.Entry {
	return GetLogger(r).WithError(err)
}

func GetLogger(r *http.Request) *log.Entry {
	return r.Context().Value("logger").(*log.Entry)
}

func setupDB() *gorm.DB {
	db, err := gorm.Open(sqlite.Open("chat.db"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		log.Fatalf("Fatal error: %v", err)
	}

	log.Println("Running migrations")

	err = models.Migrate()
	if err != nil {
		log.Fatalf("Fatal error: %v", err)
	}

	log.Println("Seeding database")

	err = models.Seed()
	if err != nil {
		log.Fatalf("Fatal error: %v", err)
	}

	return db
}
