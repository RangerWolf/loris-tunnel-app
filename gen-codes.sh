# lO4f8aqjHuLWYz-mWTEJFn6T9ul2TGjJO_bKJuzeATI

curl -X POST 'http://127.0.0.1:8000/api/v1/admin/license-codes/generate' \
  -H "Authorization: Bearer lO4f8aqjHuLWYz-mWTEJFn6T9ul2TGjJO_bKJuzeATI" \
  -H 'Content-Type: application/json' \
  -d '{"count":10,"duration_days":366,"prefix":"SH1Y-"}'


curl -X POST 'http://127.0.0.1:8000/api/v1/admin/license-codes/generate' \
  -H "Authorization: Bearer lO4f8aqjHuLWYz-mWTEJFn6T9ul2TGjJO_bKJuzeATI" \
  -H 'Content-Type: application/json' \
  -d '{"count":10,"duration_days":31,"prefix":"SH1M-"}'

 curl -X POST 'http://127.0.0.1:8000/api/v1/admin/license-codes/generate' \
  -H "Authorization: Bearer lO4f8aqjHuLWYz-mWTEJFn6T9ul2TGjJO_bKJuzeATI" \
  -H 'Content-Type: application/json' \
  -d '{"count":10,"duration_days":1,"prefix":"SH1D-"}'