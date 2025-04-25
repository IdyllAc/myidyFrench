package main

import (
    "database/sql"
    "fmt"
    "log"
    "net/http"
    "net/smtp"

    "html/template"

    _ "github.com/mattn/go-sqlite3"
    "github.com/markbates/goth"
    "github.com/markbates/goth/gothic"
    "github.com/markbates/goth/providers/facebook"
    "github.com/markbates/goth/providers/google"
)

var db *sql.DB

func main() {
    var err error
    db, err = sql.Open("sqlite3", "./subscribers.db")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()
    createTable()

    goth.UseProviders(
        facebook.New("FACEBOOK_CLIENT_ID", "FACEBOOK_CLIENT_SECRET", "http://localhost:8080/auth/facebook/callback"),
        google.New("GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "http://localhost:8080/auth/google/callback", "email", "profile"),
    )

    http.HandleFunc("/", serveHome)
    http.HandleFunc("/subscribe", handleEmailSubscription)
    http.HandleFunc("/auth/facebook", gothic.BeginAuthHandler)
    http.HandleFunc("/auth/facebook/callback", oauthCallback)
    http.HandleFunc("/auth/google", gothic.BeginAuthHandler)
    http.HandleFunc("/auth/google/callback", oauthCallback)

    fmt.Println("🚀 Server running at http://localhost:8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}

func serveHome(w http.ResponseWriter, r *http.Request) {
    t, err := template.ParseFiles("index.html")
    if err != nil {
        http.Error(w, "Error loading page", http.StatusInternalServerError)
        return
    }
    t.Execute(w, nil)
}

func handleEmailSubscription(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    email := r.FormValue("email")
    if email == "" {
        http.Error(w, "Email is required", http.StatusBadRequest)
        return
    }

    _, err := db.Exec("INSERT OR IGNORE INTO subscribers (email) VALUES (?)", email)
    if err != nil {
        http.Error(w, "Failed to save email", http.StatusInternalServerError)
        return
    }

    link := fmt.Sprintf("http://localhost:8080/verify?email=%s", email)
    go sendConfirmationEmail(email, link)

    fmt.Fprintf(w, "✅ Thanks! Confirmation sent to: %s", email)
}

func sendConfirmationEmail(to, link string) {
    from := "idyllacg@gmail.com"
    password := "ivnj kxvr hqqf qsgu"
    subject := "Please verify your email"
    body := fmt.Sprintf("Click here to confirm your subscription:\n\n%s", link)

    msg := "From: " + from + "\n" +
        "To: " + to + "\n" +
        "Subject: " + subject + "\n\n" + body

    err := smtp.SendMail("smtp.gmail.com:587",
        smtp.PlainAuth("", from, password, "smtp.gmail.com"),
        from, []string{to}, []byte(msg))

    if err != nil {
        log.Printf("❌ Failed to send confirmation email to %s: %v", to, err)
    } else {
        log.Printf("✅ Confirmation email sent to %s", to)
    }
}

func oauthCallback(w http.ResponseWriter, r *http.Request) {
    user, err := gothic.CompleteUserAuth(w, r)
    if err != nil {
        fmt.Fprintln(w, "Login failed:", err)
        return
    }
    fmt.Fprintf(w, "✅ OAuth login successful!\n\nName: %s\nEmail: %s", user.Name, user.Email)
}

func createTable() {
    query := `
    CREATE TABLE IF NOT EXISTS subscribers (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        email TEXT UNIQUE NOT NULL
    );
    `
    _, err := db.Exec(query)
    if err != nil {
        log.Fatalf("❌ Failed to create DB table: %v", err)
    }
}
