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
	Draw            string `json:"draw"`
	RecordsTotal    int    `json:"recordsTotal"`
	RecordsFiltered int    `json:"recordsFiltered"`
	Data            []Room `json:"data"`
}

type Room struct {
	Name string `json:"name"`
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
func search(field string, args []interface{}) (dataList []Room, err error) {
	var room Room
	//query = `SELECT name FROM rooms WHERE name LIKE ? ORDER BY name Limit ? , ?;`
	//rows, err := db.Raw(query, args...).Rows()

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

		rows, err = db.Debug().
			Model(&models.Room{}).
			Select(field).
			Where(fmt.Sprintf("%s LIKE ?", field), args...).
			Order(field).
			Offset(offset).
			Limit(limit).
			Rows()
		if err != nil {
			return nil, err
		}
	case noSearchValueInArgs:
		limit, offset, err := getLimitAndOffset(args)
		if err != nil {
			return nil, err
		}

		rows, err = db.Debug().
			Model(&models.Room{}).
			Select(field).
			Order(field).
			Offset(offset).
			Limit(limit).
			Rows()
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
		return nil, err
	}

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
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

			switch columns[i] {
			case "name":
				room.Name = value
			case "id":
				room.Name = value
			}
		}

		dataList = append(dataList, room)
	}

	return dataList, nil
}

func getLimitAndOffset(args []interface{}) (int, int, error) {
	limit, err := strconv.Atoi(fmt.Sprintf("%v", args[len(args)-1]))
	if err != nil {
		return 0, 0, err
	}

	offset, err := strconv.Atoi(fmt.Sprintf("%v", args[len(args)-2]))
	if err != nil {
		return 0, 0, err
	}

	return limit, offset, nil
}

var final int

func pagingHandler(w http.ResponseWriter, r *http.Request, ) {
	err := paging(w, r, []string{"name"})
	if err != nil {
		panic(err)
	}
}

func paging(w http.ResponseWriter, r *http.Request, dataTablesFields []string) error {
	var (
		pagingData PaginateDataStruct
		result     []Room
		args       []interface{}
	)

	err := r.ParseForm()
	if err != nil {
		return err
	}

	start := r.FormValue("start")
	end := r.FormValue("length")
	draw := r.FormValue("draw")
	searchValue := r.FormValue("search[value]")

	if draw == "1" {
		rows, err := db.Model(&models.Room{}).Select("COUNT(*)").Rows()
		if err != nil {
			return err
		}

		defer func() {
			_ = rows.Close()

			err = rows.Err()
		}()
		if err != nil {
			return err
		}

		for rows.Next() {
			err = rows.Scan(&final)
			if err != nil {
				return err
			}
		}
	}

	if searchValue != "" {
		//query = `SELECT name FROM rooms WHERE name LIKE ? ORDER BY name Limit ? , ?;`
		p := searchValue + "%"
		args = []interface{}{p, start, end}
		aux := []interface{}{p}
		result, err = search(dataTablesFields[0], args)
		if err != nil {
			return err
		}

		// Here we obtain the number of results
		rows, err := db.Debug().
			Model(&models.Room{}).
			Select("COUNT(*)").
			Where(generateDTWhereQuery(dataTablesFields), aux...).
			Order(dataTablesFields[0]).
			Rows()
		if err != nil {
			return err
		}

		defer func() {
			_ = rows.Close()

			err = rows.Err()
		}()

		if err != nil {
			return err
		}

		for rows.Next() {
			err = rows.Scan(&final)
			if err != nil {
				return err
			}
		}
	} else {
		//query = `SELECT name FROM rooms
		//	ORDER BY name
		//	Limit ? , ?;`
		args = []interface{}{start, end}
		result, err = search(dataTablesFields[0], args)
		if err != nil {
			return err
		}
	}

	pagingData.Data = result
	pagingData.Draw = draw
	pagingData.RecordsFiltered = final

	e, err := json.Marshal(pagingData)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_, err = w.Write(e)
	if err != nil {
		return err
	}

	return nil
}

func generateDTWhereQuery(dataTablesFields []string) string {
	whereQuery := fmt.Sprintf("%s like ? ", dataTablesFields[0])

	for _, field := range dataTablesFields[1:] {
		whereQuery += fmt.Sprintf("OR %s like ? ", field)
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
