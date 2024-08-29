# go-react-native-pos

Learning...

```
git remote add origin https://github.com/quochuydev/go-react-native-pos.git

curl -X POST -H "Content-Type: application/json" -d '{"firstname":"John", "lastName":"Doe", "phoneNumber":"1234567890"}' http://localhost:12345/api/customers

curl http://localhost:12345/api/customers

curl "http://localhost:12345/api/qrcode/generate?customer_id=66d02e9326f151753c33441d"

curl -X POST -H "Content-Type: application/json" -d '{"customer_id": "66d02e9326f151753c33441d", "code": "391121", "score": 1}' http://localhost:12345/api/qrcode/verify
```
