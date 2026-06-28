module tw-mail-engine

go 1.22

require (
	github.com/joho/godotenv v1.5.1
	go.mongodb.org/mongo-driver v1.17.1
)

// go-msgauth (firma DKIM) se añadirá cuando implementemos el módulo de envío.
