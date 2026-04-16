package server

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Password string `json:"password"` // hashed
}

type UserStore struct {
	users    map[string]*User // username -> User
	filepath string
}

func NewUserStore(filepath string) (*UserStore, error) {
	store := &UserStore{
		users:    make(map[string]*User),
		filepath: filepath,
	}
	err := store.load()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return store, nil
}

// load reads users from the JSON file
func (s *UserStore) load() error {
	data, err := os.ReadFile(s.filepath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.users)
}

// save writes users to the JSON file
func (s *UserStore) save() error {
	data, err := json.MarshalIndent(s.users, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filepath, data, 0644)
}

// ValidateEmail checks if email format is valid
func ValidateEmail(email string) bool {
	re := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	return re.MatchString(email)
}

// ValidatePhone checks if phone is valid (digits only, 7-15 chars)
func ValidatePhone(phone string) bool {
	if len(phone) < 7 || len(phone) > 15 {
		return false
	}
	for _, c := range phone {
		if !unicode.IsDigit(c) {
			return false
		}
	}
	return true
}

// ValidateUsername checks username rules
func ValidateUsername(username string) error {
	if len(username) < 3 {
		return errors.New("username must be at least 3 characters")
	}
	if len(username) > 20 {
		return errors.New("username must be at most 20 characters")
	}
	if strings.Contains(username, " ") {
		return errors.New("username cannot contain spaces")
	}
	return nil
}

// Register creates a new user
func (s *UserStore) Register(username, contact, password string) error {
	// Validate username
	if err := ValidateUsername(username); err != nil {
		return err
	}

	// Check if username already taken
	if _, exists := s.users[strings.ToLower(username)]; exists {
		return errors.New("username already taken")
	}

	// Validate contact (email or phone)
	isEmail := strings.Contains(contact, "@")
	if isEmail {
		if !ValidateEmail(contact) {
			return errors.New("invalid email format")
		}
	} else {
		if !ValidatePhone(contact) {
			return errors.New("invalid phone number: digits only, 7-15 digits")
		}
	}

	// Validate password
	if len(password) < 6 {
		return errors.New("password must be at least 6 characters")
	}

	// Hash password
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// Determine email or phone
	email, phone := "", ""
	if isEmail {
		email = contact
	} else {
		phone = contact
	}

	// Save user
	s.users[strings.ToLower(username)] = &User{
		Username: username,
		Email:    email,
		Phone:    phone,
		Password: string(hashed),
	}

	return s.save()
}

// Login checks credentials and returns the user
func (s *UserStore) Login(username, password string) (*User, error) {
	user, exists := s.users[strings.ToLower(username)]
	if !exists {
		return nil, errors.New("user not found")
	}

	err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		return nil, errors.New("wrong password")
	}

	return user, nil
}
