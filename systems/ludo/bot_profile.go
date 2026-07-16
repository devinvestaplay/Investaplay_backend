package ludo

type BotDifficulty string

const (
	BotEasy   BotDifficulty = "easy"
	BotMedium BotDifficulty = "medium"
	BotHard   BotDifficulty = "hard"
)

type BotTemplate struct {
	Name     string
	AvatarID int
	Country  string
}

type BotProfile struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	AvatarID   int           `json:"avatar_id"`
	Level      int           `json:"level"`
	Country    string        `json:"country"`
	Difficulty BotDifficulty `json:"difficulty"`
	IsBot      bool          `json:"is_bot"`
}

var ludoBotTemplates = []BotTemplate{
	{Name: "Pooja", AvatarID: 1, Country: "IN"},
	{Name: "Juhi", AvatarID: 2, Country: "IN"},
	{Name: "Aditi", AvatarID: 3, Country: "IN"},
	{Name: "Rimjhim", AvatarID: 4, Country: "IN"},
	{Name: "Payal", AvatarID: 5, Country: "IN"},
	{Name: "Anjali", AvatarID: 6, Country: "IN"},
	{Name: "Ragini", AvatarID: 7, Country: "IN"},
	{Name: "Priya", AvatarID: 8, Country: "IN"},
	{Name: "Ishiqa", AvatarID: 9, Country: "IN"},
	{Name: "Ridhima", AvatarID: 10, Country: "IN"},
	{Name: "Parul", AvatarID: 11, Country: "IN"},
	{Name: "Samiksha", AvatarID: 12, Country: "IN"},
	{Name: "Rohit", AvatarID: 13, Country: "IN"},
	{Name: "Vishal", AvatarID: 14, Country: "IN"},
	{Name: "Najmul", AvatarID: 15, Country: "IN"},
	{Name: "Meera", AvatarID: 16, Country: "IN"},
	{Name: "Ananya", AvatarID: 17, Country: "IN"},
	{Name: "Isha", AvatarID: 18, Country: "IN"},
	{Name: "Riya", AvatarID: 19, Country: "IN"},
	{Name: "Sneha", AvatarID: 20, Country: "IN"},
	{Name: "Neha", AvatarID: 21, Country: "IN"},
	{Name: "Kavya", AvatarID: 22, Country: "IN"},
	{Name: "Diya", AvatarID: 23, Country: "IN"},
	{Name: "Simran", AvatarID: 24, Country: "IN"},
	{Name: "Kriti", AvatarID: 25, Country: "IN"},
	{Name: "Naina", AvatarID: 26, Country: "IN"},
	{Name: "Tara", AvatarID: 27, Country: "IN"},
	{Name: "Shreya", AvatarID: 28, Country: "IN"},
	{Name: "Nidhi", AvatarID: 29, Country: "IN"},
	{Name: "Swati", AvatarID: 30, Country: "IN"},
	{Name: "Bhavna", AvatarID: 31, Country: "IN"},
	{Name: "Sakshi", AvatarID: 32, Country: "IN"},
	{Name: "Tanvi", AvatarID: 33, Country: "IN"},
	{Name: "Mansi", AvatarID: 34, Country: "IN"},
	{Name: "Roshni", AvatarID: 35, Country: "IN"},
	{Name: "Aarav", AvatarID: 36, Country: "IN"},
	{Name: "Arjun", AvatarID: 37, Country: "IN"},
	{Name: "Rohan", AvatarID: 38, Country: "IN"},
	{Name: "Vihaan", AvatarID: 39, Country: "IN"},
	{Name: "Kabir", AvatarID: 40, Country: "IN"},
	{Name: "Aditya", AvatarID: 41, Country: "IN"},
	{Name: "Rahul", AvatarID: 42, Country: "IN"},
	{Name: "Karan", AvatarID: 43, Country: "IN"},
	{Name: "Dev", AvatarID: 44, Country: "IN"},
	{Name: "Aryan", AvatarID: 45, Country: "IN"},
	{Name: "Aman", AvatarID: 46, Country: "IN"},
	{Name: "Nikhil", AvatarID: 47, Country: "IN"},
	{Name: "Varun", AvatarID: 48, Country: "IN"},
	{Name: "Mohit", AvatarID: 49, Country: "IN"},
	{Name: "Ishaan", AvatarID: 50, Country: "IN"},
}
