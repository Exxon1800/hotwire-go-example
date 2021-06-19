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
	dtColumnName string
	databaseName string
}

var final int
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

func pagingHandler(w http.ResponseWriter, r *http.Request, ) {
	err := pagination(w, r, "rooms", []dataTablesField{{"name", "name"}})
	if err != nil {
		panic(err)
	}
}

// pagination is responsible for the datatables pagination
func pagination(w http.ResponseWriter, r *http.Request, tableName string, dtFields []dataTablesField) error {
	var (
		pagingData PaginateDataStruct
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
		err := getFirstDrawResults(tableName)
		if err != nil {
			return err
		}
	}

	if searchValue != "" {
		pagingData.Data, err = getNonFilteredResults(searchValue, start, end, dtFields, tableName)
		if err != nil {
			return err
		}
	} else {
		pagingData.Data, err = getFilteredResults([]interface{}{start, end}, dtFields, tableName)
		if err != nil {
			return err
		}
	}

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

// getFilteredResults will get the results where the search-box on the front-end contains a search value
func getFilteredResults(args []interface{}, dtFields []dataTablesField, tableName string) ([]map[string]string, error) {
	var (
		result []map[string]string
		p      = "%"
		aux    = []interface{}{p}
	)

	result, err := search(dtFields[0], dtFields, tableName, args)
	if err != nil {
		return nil, err
	}

	err = getNumberOfResults(err, dtFields, aux)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// getNonFilteredResults will get the results where the search-box on the front-end is empty
func getNonFilteredResults(
	searchValue string, start string, end string, dtFields []dataTablesField, tableName string,
) ([]map[string]string, error) {
	var (
		p    = searchValue + "%"
		args = []interface{}{p, start, end}
		aux  = []interface{}{p}
	)

	result, err := search(dtFields[0], dtFields, tableName, args)
	if err != nil {
		return nil, err
	}

	err = getNumberOfResults(err, dtFields, aux)
	if err != nil {
		return nil, err
	}

	return result, err
}

// getNumberOfResults obtains the number of results/entries in the dataTable
func getNumberOfResults(err error, dtFields []dataTablesField, aux []interface{}) error {
	rows, err := db.Model(&models.Room{}).
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

	return nil
}

// getFirstDrawResults gets the result for the first draw of the datatables/first load of the page
func getFirstDrawResults(tableName string) error {
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

	return nil
}

// search function returns the result of the query
func search(
	sortingField dataTablesField, dtFields []dataTablesField, tableName string, args []interface{},
) ([]map[string]string, error) {
	var (
		err                    error
		rows                   *sql.Rows
		noSearchArgumentInArgs = 2
		searchArgumentInArgs   = 3
	)

	switch len(args) {
	case searchArgumentInArgs:
		rows, err = getResultRows(sortingField, args, tableName)
		if err != nil {
			return nil, err
		}
	case noSearchArgumentInArgs:
		rows, err = getResultRowsWithSearchArguments(sortingField, args, tableName)
		if err != nil {
			return nil, err
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

	paginationDataList, err := databaseRowsToPaginationDataList(rows, dtFields)
	if err != nil {
		return nil, err
	}

	return paginationDataList, nil
}

// databaseRowsToPaginationDataList converts the result rows to a map
// this map will only contain fields found in the dataTablesField.dtColumnName (columns that are used in the dataTablet)
func databaseRowsToPaginationDataList(rows *sql.Rows, dtFields []dataTablesField) ([]map[string]string, error) {
	var dataList []map[string]string

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
			return nil, fmt.Errorf("could not scan rows to 'scanArgs...' %w", err)
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
					dtObject := map[string]string{dtField.dtColumnName: value}
					dataList = append(dataList, dtObject)
				}
			}
		}
	}

	return dataList, nil
}

// getResultRows gets the results from the database
// using search arguments from the datatables search-box in the WHERE of the query
func getResultRowsWithSearchArguments(
	sortingField dataTablesField, args []interface{}, tableName string,
) (*sql.Rows, error) {
	limit, offset, err := getLimitAndOffset(args)
	if err != nil {
		return nil, err
	}

	rows, err := db.Table(tableName).
		Select(sortingField.databaseName).
		Order(sortingField.databaseName).
		Offset(offset).
		Limit(limit).
		Rows()
	if err != nil {
		return nil, fmt.Errorf("could not execute search query %w", err)
	}

	return rows, nil
}

// getResultRows gets the results from the database
func getResultRows(sortingField dataTablesField, args []interface{}, tableName string) (*sql.Rows, error) {
	limit, offset, err := getLimitAndOffset(args)
	if err != nil {
		return nil, err
	}

	rows, err := db.Table(tableName).
		Select(sortingField.databaseName).
		Where(fmt.Sprintf("%s LIKE ?", sortingField.databaseName), args...).
		Order(sortingField.databaseName).
		Offset(offset).
		Limit(limit).
		Rows()
	if err != nil {
		return nil, fmt.Errorf("could not execute filtered search query %w", err)
	}

	return rows, nil
}

// getLimitAndOffset gets the limit and offset out of the args passed in the post request created by datatables
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

// generateDTWhereQuery generates the WHERE part of the queries used for the dataTables pagination
func generateDTWhereQuery(dtFields []dataTablesField) string {
	whereQuery := fmt.Sprintf("%s like ? ", dtFields[0].databaseName)

	for _, field := range dtFields[1:] {
		whereQuery += fmt.Sprintf("OR %s like ? ", field.databaseName)
	}

	return whereQuery
}

//nolint:nolintlint,godox    // TODO only for this test setup!
func indexHandler(w http.ResponseWriter, r *http.Request) {
	p := Page{
		Title: "pagination",
	}

	t, err := template.ParseFiles("templates/index.html")
	ifErrorToPage(w, r, err)

	err = t.Execute(w, p)
	ifErrorToPage(w, r, err)
}

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
