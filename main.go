package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"net/url"
	"os"

	"github.com/gorilla/sessions"
	"github.com/joho/godotenv"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/facebook"
	"github.com/markbates/goth/providers/google"
	"github.com/markbates/goth/providers/github"

	_ "github.com/mattn/go-sqlite3" // Use this or modernc.org/sqlite
)

var db *sql.DB

func main() {
	godotenv.Load()

	// Load SESSION_SECRET from env file
	key := os.Getenv("SESSION_SECRET")
	if key == "" {
		log.Fatal("❌ SESSION_SECRET is missing in .env")
	}

	store := sessions.NewCookieStore([]byte(key))
	store.MaxAge(86400 * 30) // 30 days
	store.Options.HttpOnly = true
	store.Options.Secure = false
	gothic.Store = store

	// Initialize OAuth providers
	goth.UseProviders(
		facebook.New(
			os.Getenv("FACEBOOK_KEY"),
			os.Getenv("FACEBOOK_SECRET"),
			"http://localhost:8080/auth/facebook/callback",
		),
		google.New(
			os.Getenv("GOOGLE_KEY"),
			os.Getenv("GOOGLE_SECRET"),
			"http://localhost:8080/auth/google/callback",
			"email", "profile",
		),
		github.New(
			os.Getenv("GITHUB_KEY"),
			os.Getenv("GITHUB_SECRET"),
			"http://localhost:8080/auth/github/callback",
		),
	)

	// Connect to SQLite DB
	var err error
	db, err = sql.Open("sqlite3", "./DB_subscribers.db")
	if err != nil {
		log.Fatal("❌ DB connection failed:", err)
	}
	defer db.Close()

	createTables()

	// Routes
	http.Handle("/", http.FileServer(http.Dir("./static")))
	http.HandleFunc("/subscribe", serveSubscribe)
	http.HandleFunc("/subscribe/email", handleEmailSubscription)
	http.HandleFunc("/subscribers", handleListSubscribers)
	http.HandleFunc("/view-emails", handleViewEmails)
	http.HandleFunc("/submit", handleFormSubmission)

	// OAuth Routes
	http.HandleFunc("/auth/facebook", handleFacebookLogin)
	http.HandleFunc("/auth/facebook/callback", handleFacebookCallback)
	http.HandleFunc("/auth/google", handleGoogleLogin)
	http.HandleFunc("/auth/google/callback", handleGoogleCallback)
	http.HandleFunc("/auth/github", handleGitHubLogin)
	http.HandleFunc("/auth/github/callback", handleGitHubCallback)

	fmt.Println("✅ Server running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func createTables() {
	subscriberTable := `
	CREATE TABLE IF NOT EXISTS subscribers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT UNIQUE NOT NULL
	);`
	messageTable := `
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		subscriber_id INTEGER,
		message TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(subscriber_id) REFERENCES subscribers(id)
	);`
	_, err := db.Exec(subscriberTable)
	if err != nil {
		log.Fatal("❌ Failed to create subscribers table:", err)
	}
	_, err = db.Exec(messageTable)
	if err != nil {
		log.Fatal("❌ Failed to create messages table:", err)
	}
}

func serveSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		http.ServeFile(w, r, "./static/subscribe.html")
	} else {
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
	}
}

func handleEmailSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
		return
	}
	email := r.FormValue("email")
	message := r.FormValue("message")

	if email == "" || message == "" {
		http.Error(w, "Email and message are required", http.StatusBadRequest)
		return
	}

	// Step 1: Insert or ignore subscriber
	_, err := db.Exec("INSERT OR IGNORE INTO subscribers(email) VALUES(?)", email)
	if err != nil {
		http.Error(w, "❌ Could not save email: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Step 2: Get subscriber ID
	var id int
	err = db.QueryRow("SELECT id FROM subscribers WHERE email = ?", email).Scan(&id)
	if err != nil {
		http.Error(w, "❌ Could not fetch ID: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Step 3: Insert message
	_, err = db.Exec("INSERT INTO messages(subscriber_id, message) VALUES(?, ?)", id, message)
	if err != nil {
		http.Error(w, "❌ Could not save message: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Step 4: Save email to .txt file
	f, err := os.OpenFile("subscribers_emails.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		f.WriteString(email + "\n")
	}

	// Step 5: Send confirmation email
	link := "http://localhost:8080/verify?email=" + url.QueryEscape(email)
	sendConfirmationEmail(email, link)

	fmt.Fprintf(w, "✅ Thanks %s! Confirmation sent.", email)
}

func sendConfirmationEmail(to, link string) {
	from := os.Getenv("SMTP_EMAIL")
	password := os.Getenv("SMTP_PASS")

	subject := "Please verify your email"
	body := fmt.Sprintf("Click the link to confirm:\n%s", link)

	msg := "From: " + from + "\nTo: " + to + "\nSubject: " + subject + "\n\n" + body

	err := smtp.SendMail("smtp.gmail.com:587",
		smtp.PlainAuth("", from, password, "smtp.gmail.com"),
		from, []string{to}, []byte(msg))

	if err != nil {
		log.Println("❌ Email send failed:", err)
	} else {
		log.Println("✅ Email sent to:", to)
	}
}

func handleListSubscribers(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT email FROM subscribers")
	if err != nil {
		http.Error(w, "Failed to fetch subscribers", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var email string
		rows.Scan(&email)
		fmt.Fprintln(w, email)
	}
}

func handleViewEmails(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile("subscribers_emails.txt")
	if err != nil {
		http.Error(w, "❌ Cannot read file", http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func handleFormSubmission(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		r.ParseForm()
		email := r.FormValue("email")
		message := r.FormValue("message")
		fmt.Printf("📩 New message from %s: %s\n", email, message)
		w.Write([]byte("✅ Message received!"))
	} else {
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
	}
}

// OAuth handlers
func handleFacebookLogin(w http.ResponseWriter, r *http.Request) {
	r.URL.RawQuery = "provider=facebook"
	gothic.BeginAuthHandler(w, r)
}
func handleFacebookCallback(w http.ResponseWriter, r *http.Request) {
	user, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		http.Error(w, "Facebook login failed", http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, "✅ Logged in via Facebook\nName: %s\nEmail: %s", user.Name, user.Email)
}

func handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	r.URL.RawQuery = "provider=google"
	gothic.BeginAuthHandler(w, r)
}
func handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	user, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		http.Error(w, "Google login failed", http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, "✅ Logged in via Google\nName: %s\nEmail: %s", user.Name, user.Email)
}

func handleGitHubLogin(w http.ResponseWriter, r *http.Request) {
	r.URL.RawQuery = "provider=github"
	gothic.BeginAuthHandler(w, r)
}
func handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	user, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		http.Error(w, "GitHub login failed", http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, "✅ Logged in via GitHub\nName: %s\nEmail: %s", user.Name, user.Email)
}
