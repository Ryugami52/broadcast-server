package server

import (
	"context"
	"errors"
	"time"
	"unicode"

	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	Username  string    `bson:"username"`
	Email     string    `bson:"email"`
	Phone     string    `bson:"phone"`
	Password  string    `bson:"password"`
	CreatedAt time.Time `bson:"created_at"`
}

type UserStore struct {
	collection *mongo.Collection
}

func NewUserStore(mongoURI, dbName string) (*UserStore, *mongo.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, nil, err
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, nil, err
	}

	collection := client.Database(dbName).Collection("users")

	_, err = collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "username", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, nil, err
	}

	return &UserStore{collection: collection}, client, nil
}

func ValidateEmail(email string) bool {
	if len(email) > 254 || strings.Contains(email, " ") {
		return false
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 || len(parts[0]) == 0 {
		return false
	}
	domainParts := strings.Split(parts[1], ".")
	if len(domainParts) < 2 {
		return false
	}
	for _, p := range domainParts {
		if len(p) == 0 {
			return false
		}
	}
	return true
}

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

func (s *UserStore) Register(username, contact, password string) error {
	if err := ValidateUsername(username); err != nil {
		return err
	}

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

	if len(password) < 6 {
		return errors.New("password must be at least 6 characters")
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	email, phone := "", ""
	if isEmail {
		email = contact
	} else {
		phone = contact
	}

	user := User{
		Username:  username,
		Email:     email,
		Phone:     phone,
		Password:  string(hashed),
		CreatedAt: time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = s.collection.InsertOne(ctx, user)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return errors.New("username already taken")
		}
		return err
	}

	return nil
}

func (s *UserStore) Login(username, password string) (*User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var user User
	err := s.collection.FindOne(ctx, bson.M{"username": username}).Decode(&user)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errors.New("user not found")
		}
		return nil, err
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		return nil, errors.New("wrong password")
	}

	return &user, nil
}

type ChatMessage struct {
	Room      string    `bson:"room"` // "public" or "private:user1:user2"
	Sender    string    `bson:"sender"`
	Content   string    `bson:"content"`
	Timestamp time.Time `bson:"timestamp"`
}

type HistoryStore struct {
	collection *mongo.Collection
}

func NewHistoryStore(client *mongo.Client, dbName string) *HistoryStore {
	collection := client.Database(dbName).Collection("messages")
	return &HistoryStore{collection: collection}
}

func (h *HistoryStore) Save(room, sender, content string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := h.collection.InsertOne(ctx, ChatMessage{
		Room:      room,
		Sender:    sender,
		Content:   content,
		Timestamp: time.Now(),
	})
	return err
}

func (h *HistoryStore) GetLast(room string, limit int) ([]ChatMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := h.collection.Find(ctx, bson.M{"room": room}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var messages []ChatMessage
	if err := cursor.All(ctx, &messages); err != nil {
		return nil, err
	}

	// Reverse so oldest is first
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}
