package main

import (
	"database/sql"
	"encoding/json"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"github.com/while1malloc0/hotwire-go-example/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"html/template"
	"io"
	"net/http"
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
	r.HandleFunc("/populateDataTable", paging).Methods("POST")
	http.Handle("/", r)
	_ = http.ListenAndServe(":8080", nil)
}

// search function returns the result of the query
func search(query string, args []interface{}) (dataList []Room) {
	var room Room

	rows, err := db.Raw(query, args...).Rows()
	if err != nil {
		panic(err)
	}

	defer func() {
		_ = rows.Close()

		err = rows.Err()
		if err != nil {
			log.Fatalf("Fatal error: %v", err)
		}
	}()

	columns, err := rows.Columns()
	if err != nil {
		panic(err)
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

	return dataList
}

// Here we store the recordsTotal and recordsFiltered value
var final int

func paging(w http.ResponseWriter, r *http.Request) {
	var (
		paging       PaginateDataStruct
		result       []Room
		count, query string
		args         []interface{}
	)

	err := r.ParseForm()
	if err != nil {
		log.Fatalf("Fatal error: %v", err)
	}

	count = `SELECT count(*) as frequency FROM rooms`

	start := r.FormValue("start")
	end := r.FormValue("length")
	draw := r.FormValue("draw")
	searchValue := r.FormValue("search[value]")

	if draw == "1" {
		rows, err := db.Raw(count).Rows()
		if err != nil {
			log.Fatalf("Fatal error: %v", err)
		}

		defer func() {
			_ = rows.Close()

			err = rows.Err()
			if err != nil {
				log.Fatalf("Fatal error: %v", err)
			}
		}()

		for rows.Next() {
			err = rows.Scan(&final)
			if err != nil {
				log.Panicf("Fatal error: %v", err)
			}
		}
	}

	if searchValue != "" {
		query = `SELECT name FROM rooms
						WHERE name LIKE ? 
						ORDER BY name
						Limit ? , ?;`
		p := searchValue + "%"
		args = []interface{}{p, start, end}
		aux := []interface{}{p}
		result = search(query, args)

		// Here we obtain the number of results
		rows, err := db.Raw(`SELECT COUNT(*) FROM rooms
			WHERE name LIKE ? 
			ORDER BY name`, aux...).Rows()
		if err != nil {
			log.Fatalf("Fatal error: %v", err)
		}

		defer func() {
			_ = rows.Close()

			err = rows.Err()
			if err != nil {
				log.Fatalf("Fatal error: %v", err)
			}
		}()

		for rows.Next() {
			err = rows.Scan(&final)
			if err != nil {
				log.Fatalf("Fatal error: %v", err)
			}
		}
	} else {
		query = `SELECT name FROM rooms
			ORDER BY name
			Limit ? , ?;`
		args = []interface{}{start, end}
		result = search(query, args)
	}

	paging.Data = result
	paging.Draw = draw
	paging.RecordsFiltered = final
	
	e, err := json.Marshal(paging)
	if err != nil {
		log.Fatalf("Fatal error: %v", err)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(e)
	if err != nil {
		return 
	}

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
