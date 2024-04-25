package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"html/template"
)

type Config struct {
	DBUser     string `json:"dbUser"`
	DBPassword string `json:"dbPassword"`
	DBName     string `json:"dbName"`
}

var db *sql.DB
var config Config

type Room struct {
	ID       int
	Name     string
	Capacity int
}

type Reservation struct {
	ID        int
	RoomName  string
	Date      string
	StartTime string
	EndTime   string
}

func initDB() {
	dataSourceName := fmt.Sprintf("%s:%s@/%s", config.DBUser, config.DBPassword, config.DBName)
	var err error
	db, err = sql.Open("mysql", dataSourceName)
	if err != nil {
		log.Fatal(err)
	}

	err = db.Ping()
	if err != nil {
		log.Fatal(err)
	}
}

var templates *template.Template

func init() {
	templates = template.Must(template.ParseFiles("templates/home.html", "templates/reserve.html", "templates/rooms.html", "templates/listReservation.html"))
}
func loadConfig(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		return err
	}
	return nil
}
func main() {
	err := loadConfig("config.json")
	if err != nil {
		log.Fatal("Error loading config:", err)
	}

	initDB()
	defer db.Close()

	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/reserve", reserveHandler)
	http.HandleFunc("/rooms", listAvailableRoomsHandler)
	http.HandleFunc("/cancel", cancelReservationHandler)
	http.HandleFunc("/listReservation", listReservationsHandler)

	// Start the web server
	log.Println("Starting server on :8000...")
	err = http.ListenAndServe(":8000", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

func displayMainMenu() {
	var roomName, date, startTime, endTime string
	fmt.Println("-----------------------------------------------------")
	fmt.Println("1. Lister les salles disponibles")
	fmt.Println("2. Créer une réservation")
	fmt.Println("3. Annuler une réservation")
	fmt.Println("4. Voir les réservations")
	fmt.Println("5. Quitter")
	fmt.Print("Choisissez une option : ")

	var choice int
	fmt.Scanln(&choice)

	switch choice {
	case 1:
		listAvailableRooms(date, startTime)
	case 2:
		createReservation(roomName, date, startTime, endTime)
	case 3:
		CancelReservation()
	case 4:
		ListReservations(db, roomName, date)
	case 5:
		fmt.Println("Merci d'utiliser notre service !")
		os.Exit(0)
	default:
		fmt.Println("Option invalide. Veuillez réessayer.")
	}
}

// Case 1 : Lister les salles disponibles

func listAvailableRooms(inputDate, inputTime string) ([]Room, error) {
	if inputDate == "" {
		fmt.Print("Entrez la date (YYYY-MM-DD) : ")
		fmt.Scanln(&inputDate)
	}

	if inputTime == "" {
		fmt.Print("Entrez l'heure (HH:MM) : ")
		fmt.Scanln(&inputTime)
	}

	date, err := time.Parse("2006-01-02", inputDate)
	if err != nil {
		return nil, fmt.Errorf("Format de date invalide. Veuillez utiliser le format YYYY-MM-DD : %v", err)
	}

	timeSlot, err := time.Parse("15:04", inputTime)
	if err != nil {
		return nil, fmt.Errorf("Format d'heure invalide. Veuillez utiliser le format HH:MM : %v", err)
	}

	fmt.Printf("Salles disponibles pour le %s à %s :\n", date.Format("2006-01-02"), timeSlot.Format("15:04"))
	rows, err := db.Query("SELECT id, name, capacity FROM rooms")
	if err != nil {
		return nil, fmt.Errorf("Erreur lors de la récupération des salles : %v", err)
	}
	defer rows.Close()

	var availableRooms []Room

	for rows.Next() {
		var room Room
		if err := rows.Scan(&room.ID, &room.Name, &room.Capacity); err != nil {
			return nil, fmt.Errorf("Erreur lors de la lecture des données de la salle : %v", err)
		}

		if isRoomAvailableList(room.ID, date, timeSlot) {
			availableRooms = append(availableRooms, room)
		}
	}

	if len(availableRooms) > 0 {
		for i, room := range availableRooms {
			fmt.Printf("%d. %s (Capacité : %d)\n", i+1, room.Name, room.Capacity)
		}
	} else {
		fmt.Println("Aucune salle disponible pour ce créneau horaire.")
	}

	return availableRooms, nil
}

func isRoomAvailableList(roomID int, date, timeSlot time.Time) bool {
	startTime := timeSlot.Format("15:04:05")
	endTime := timeSlot.Add(time.Hour).Format("15:04:05")

	query := `
		SELECT COUNT(*)
		FROM reservations
		WHERE room_id = ? 
			AND date = ?
			AND (
				(start_time <= ? AND end_time > ?) OR
				(start_time < ? AND end_time >= ?)
			)
	`

	var count int
	err := db.QueryRow(query, roomID, date.Format("2006-01-02"), startTime, startTime, endTime, endTime).Scan(&count)
	if err != nil {
		log.Fatal("Échec de la vérification de la disponibilité de la salle:", err)
	}

	return count == 0
}

// Case 2 : Créer une réservation

func isRoomAvailable(roomID int, date, startTime, endTime time.Time) bool {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM reservations WHERE room_id = ? AND date = ? AND ((start_time <= ? AND end_time > ?) OR (start_time < ? AND end_time >= ?))", roomID, date.Format("2006-01-02"), startTime.Format("15:04:05"), startTime.Format("15:04:05"), endTime.Format("15:04:05"), endTime.Format("15:04:05")).Scan(&count)
	if err != nil {
		log.Fatal("Échec de la vérification de la disponibilité de la salle:", err)
	}
	return count == 0
}
func getRoomIDByName(name string) (int, error) {
	var roomID int
	err := db.QueryRow("SELECT id FROM rooms WHERE name = ?", name).Scan(&roomID)
	if err != nil {
		return 0, err
	}
	return roomID, nil
}

func parseDate(inputDate string) (time.Time, error) {
	date, err := time.Parse("2006-01-02", inputDate)
	if err != nil {
		return time.Time{}, err
	}

	if date.Year() < 1 || date.Year() > 9999 {
		return time.Time{}, fmt.Errorf("l'année n'est pas comprise dans la plage [1, 9999]: %d", date.Year())
	}
	return date, nil
}
func createReservation(roomName, date, startTime, endTime string) (string, error) {
	parsedDate, err := parseDate(date)
	if err != nil {
		return "", fmt.Errorf("erreur lors de l'analyse de la date : %v", err)
	}

	parsedStartTime, err := time.Parse("15:04", startTime)
	if err != nil {
		return "", fmt.Errorf("format de l'heure de début invalide. Veuillez utiliser le format HH:MM")
	}

	parsedEndTime, err := time.Parse("15:04", endTime)
	if err != nil {
		return "", fmt.Errorf("format de l'heure de fin invalide. Veuillez utiliser le format HH:MM")
	}

	roomID, err := getRoomIDByName(roomName)
	if err != nil {
		return "", fmt.Errorf("échec de récupération de l'ID de la salle : %v", err)
	}

	if !isRoomAvailable(roomID, parsedDate, parsedStartTime, parsedEndTime) {
		return "", fmt.Errorf("la salle sélectionnée n'est pas disponible pour la date et les heures spécifiées")
	}

	startTimeFormatted := parsedStartTime.Format("15:04:05")
	endTimeFormatted := parsedEndTime.Format("15:04:05")

	_, err = db.Exec("INSERT INTO reservations (room_id, date, start_time, end_time) VALUES (?, ?, ?, ?)", roomID, parsedDate, startTimeFormatted, endTimeFormatted)
	if err != nil {
		return "", fmt.Errorf("échec de la création de la réservation : %v", err)
	}

	successMessage := "Réservation créée avec succès !"
	return successMessage, nil
}

// Case 3 : Annuler une réservation

func CancelReservation() {
	var reservationID int
	fmt.Print("Identifiant de la Réservation : ")
	fmt.Scanln(&reservationID)

	//recherche de la Réservation
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM reservations WHERE id = ?)", reservationID).Scan(&exists)
	if err != nil {
		log.Fatal("Erreur lors de la recherche de la réservation :", err)
	}

	if !exists {
		fmt.Println("La réservation avec cet identifiant n'a pas été trouvée.")
		return
	}

	//annulation de la réservation
	_, err = db.Exec("DELETE FROM reservations WHERE id = ?", reservationID)
	if err != nil {
		log.Fatal("Erreur lors de l'annulation de la réservation :", err)
	}

	fmt.Println("La réservation a été annulée avec succès.")
}

// Case 4 : Visualiser les réservations
func ListReservations(db *sql.DB, roomName string, date string) ([]Reservation, error) {
	var query string
	var args []interface{}

	if roomName != "" && date != "" {
		query = "SELECT ro.name, DATE_FORMAT(r.date, '%Y-%m-%d'), TIME_FORMAT(r.start_time, '%H:%i'), TIME_FORMAT(r.end_time, '%H:%i') FROM reservations r INNER JOIN rooms ro ON r.room_id = ro.id WHERE ro.name = ? AND r.date = ? ORDER BY r.date, r.start_time"
		args = append(args, roomName, date)
	} else if roomName != "" {
		query = "SELECT ro.name, DATE_FORMAT(r.date, '%Y-%m-%d'), TIME_FORMAT(r.start_time, '%H:%i'), TIME_FORMAT(r.end_time, '%H:%i') FROM reservations r INNER JOIN rooms ro ON r.room_id = ro.id WHERE ro.name = ? ORDER BY r.date, r.start_time"
		args = append(args, roomName)
	} else if date != "" {
		query = "SELECT ro.name, DATE_FORMAT(r.date, '%Y-%m-%d'), TIME_FORMAT(r.start_time, '%H:%i'), TIME_FORMAT(r.end_time, '%H:%i') FROM reservations r INNER JOIN rooms ro ON r.room_id = ro.id WHERE r.date = ? ORDER BY r.date, r.start_time"
		args = append(args, date)
	} else {
		query = "SELECT ro.name, DATE_FORMAT(r.date, '%Y-%m-%d'), TIME_FORMAT(r.start_time, '%H:%i'), TIME_FORMAT(r.end_time, '%H:%i') FROM reservations r INNER JOIN rooms ro ON r.room_id = ro.id ORDER BY ro.name, r.date, r.start_time"
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reservations []Reservation
	for rows.Next() {
		var res Reservation
		if err := rows.Scan(&res.RoomName, &res.Date, &res.StartTime, &res.EndTime); err != nil {
			return nil, err
		}
		reservations = append(reservations, res)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reservations, nil
}

func displayNavigationOptions() {
	fmt.Println("1. Retourner au menu principal")
	fmt.Println("2. Quitter")
	fmt.Print("Choisissez une option : ")

	var choice int
	fmt.Scanln(&choice)

	switch choice {
	case 1:
		displayMainMenu()
	case 2:
		fmt.Println("Merci d'utiliser notre service !")
		os.Exit(0)
	default:
		fmt.Println("Option invalide. Veuillez réessayer.")
		displayNavigationOptions()
	}
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "home", nil)
}
func reserveHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		renderTemplate(w, "reserve", nil)
	case "POST":
		err := r.ParseForm()
		if err != nil {
			http.Error(w, "Error parsing form", http.StatusBadRequest)
			return
		}

		roomName := r.FormValue("roomName")
		date := r.FormValue("date")
		startTime := r.FormValue("startTime")
		endTime := r.FormValue("endTime")

		successMessage, err := createReservation(roomName, date, startTime, endTime)
		if err != nil {
			http.Error(w, "Failed to create reservation: "+err.Error(), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/success?message="+url.QueryEscape(successMessage), http.StatusSeeOther)
	default:
		http.Error(w, "Method not supported", http.StatusMethodNotAllowed)
	}
}
func listAvailableRoomsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	inputDate := r.URL.Query().Get("inputDate")
	inputTime := r.URL.Query().Get("inputTime")

	if inputDate == "" || inputTime == "" {
		renderTemplate(w, "rooms", nil)
		return
	}

	rooms, err := listAvailableRooms(inputDate, inputTime)
	if err != nil {
		http.Error(w, "Internal Server Error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Rooms []Room
	}{
		Rooms: rooms,
	}
	renderTemplate(w, "rooms", data)
}
func listReservationsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get parameters from the request
	roomName := r.URL.Query().Get("roomName")
	date := r.URL.Query().Get("date")

	// Call the refactored ListReservations function
	reservations, err := ListReservations(db, roomName, date)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := templates.ExecuteTemplate(w, "listReservation.html", reservations); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
	}
}

func cancelReservationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		renderTemplate(w, "cancel", nil)
	} else if r.Method == "POST" {
	}
}

func renderTemplate(w http.ResponseWriter, tmpl string, data interface{}) {
	err := templates.ExecuteTemplate(w, tmpl+".html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
