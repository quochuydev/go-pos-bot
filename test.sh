curl -X POST -H "Content-Type: application/json" -d '{"firstname":"John", "lastName":"Doe", "phoneNumber":"1234567890", "score": 0}' http://localhost:12345/api/customers

curl http://localhost:12345/api/customers

curl "http://localhost:12345/api/qrcode/generate?customer_id=66d02e9326f151753c33441d"

curl -X POST -H "Content-Type: application/json" -d '{"code": "889351", "score": 1}' http://localhost:12345/api/qrcode/verify

curl http://localhost:12345/api/customers