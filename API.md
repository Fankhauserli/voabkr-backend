# API Documentation

Comprehensive reference for the `voabkr-backend` HTTP API, including authentication mechanisms, request/response Go structs, JSON schemas, status codes, and example requests.

---

## 1. Overview & Authentication

- **Base URL**: `http://localhost:8080` (default)
- **API Prefix**: `/api/v1`
- **Default Headers**:
  - `Content-Type: application/json`
  - `Accept: application/json`

### Session Authentication via Redis
Authentication uses cookie-based sessions backed by Redis (`gin-contrib/sessions/redis`):

- **Cookie Name**: `userSession`
- **Cookie Properties**:
  - `Path: /`
  - `MaxAge: 86400` (24 hours)
  - `HttpOnly: true` (prevents client-side script access)
  - `Secure: true` (enforced over HTTPS)
  - `SameSite: Lax`
- **Protected Routes**: Protected endpoints use `middleware.AuthMiddleware()`. If the session is missing or expired, the server aborts with `401 Unauthorized`.
- **How to authenticate requests**:
  - When you call `POST /api/v1/login` or `POST /api/v1/register`, the response includes a `Set-Cookie: userSession=...` header.
  - Subsequent requests must include this cookie.

#### Using `curl`
```bash
# 1. Log in and store the session cookie to a file
curl -X POST http://localhost:8080/api/v1/login \
  -c cookies.txt \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"mypassword"}'

# 2. Make authenticated requests using the stored cookie
curl http://localhost:8080/api/v1/user/profile \
  -b cookies.txt \
  -H "Accept: application/json"
```

#### Using Fetch (Browser / Frontend)
```javascript
// Ensure credentials: 'include' is specified so cookies are transmitted
const res = await fetch('http://localhost:8080/api/v1/decks/', {
  method: 'GET',
  credentials: 'include',
  headers: {
    'Accept': 'application/json'
  }
});
const data = await res.json();
```

---

## 2. Standard Error Format

All error responses return a JSON object with an `error` message and an optional `details` field when running in Gin debug mode:

```json
{
  "error": "Invalid request body",
  "details": "Key: 'RegisterRequestBody.Email' Error:Field validation for 'Email' failed on the 'email' tag"
}
```

---

## 3. Endpoints Reference

### 3.1 Health & Readiness Probes

#### `GET /healthz`
Liveness check for container orchestration and load balancers.
- **Auth**: None
- **Response `200 OK`**:
  ```json
  { "status": "ok" }
  ```

#### `GET /readyz`
Readiness check.
- **Auth**: None
- **Response `200 OK`**:
  ```json
  { "status": "ok" }
  ```

---

### 3.2 Authentication (`/api/v1`)

#### `POST /api/v1/register`
Registers a new user account, creates a session, and sends an email verification token if SMTP is configured.
- **Auth**: None
- **Go Request Struct** (`types.RegisterRequestBody`):
  ```go
  type RegisterRequestBody struct {
      Name     string `json:"name" binding:"required"`
      Email    string `json:"email" binding:"required,email"`
      Password string `json:"password" binding:"required"`
  }
  ```
- **Example Request**:
  ```bash
  curl -X POST http://localhost:8080/api/v1/register \
    -c cookies.txt \
    -H "Content-Type: application/json" \
    -d '{
      "name": "Jane Doe",
      "email": "jane@example.com",
      "password": "SecretPassword123!"
    }'
  ```
- **Responses**:
  - `200 OK`: `{"message": "Registered successfully"}` (+ `Set-Cookie: userSession=...`)
  - `400 Bad Request`: `{"error": "Invalid request body"}`
  - `409 Conflict`: `{"error": "User with this email already exists"}`
  - `500 Internal Server Error`: `{"error": "Failed to create user"}` or `{"error": "Failed to send verification email"}`

#### `POST /api/v1/login`
Authenticates user credentials and starts a Redis-backed session.
- **Auth**: None
- **Go Request Struct** (`types.LoginRequestBody`):
  ```go
  type LoginRequestBody struct {
      Email    string `json:"email" binding:"required,email"`
      Password string `json:"password" binding:"required"`
  }
  ```
- **Example Request**:
  ```bash
  curl -X POST http://localhost:8080/api/v1/login \
    -c cookies.txt \
    -H "Content-Type: application/json" \
    -d '{
      "email": "jane@example.com",
      "password": "SecretPassword123!"
    }'
  ```
- **Responses**:
  - `200 OK`: `{"message": "Logged in successfully"}` (+ `Set-Cookie: userSession=...`)
  - `400 Bad Request`: `{"error": "Invalid request body"}`
  - `401 Unauthorized`: `{"error": "Invalid email or password"}`
  - `500 Internal Server Error`: `{"error": "Failed to create session"}`

#### `POST /api/v1/logout`
Destroys the current session in Redis and clears the client cookie.
- **Auth**: **Required**
- **Example Request**:
  ```bash
  curl -X POST http://localhost:8080/api/v1/logout -b cookies.txt
  ```
- **Responses**:
  - `200 OK`: `{"message": "Logged out successfully"}`
  - `401 Unauthorized`: `{"error": "Unauthorized"}`
  - `500 Internal Server Error`: `{"error": "Failed to terminate session"}`

#### `POST /api/v1/verification/:token`
Verifies the user's email address using the one-time 64-character token sent by email.
- **Auth**: None
- **URL Parameters**:
  - `:token` (path parameter, string)
  - Alternatively: `?token=<token>` (query parameter fallback)
- **Example Request**:
  ```bash
  curl -X POST http://localhost:8080/api/v1/verification/a1b2c3d4e5f6...
  ```
- **Responses**:
  - `200 OK`: `{"message": "Email verified successfully"}`
  - `400 Bad Request`: `{"error": "Token is required"}`, `{"error": "Invalid or expired token"}`, or `{"error": "Token has expired"}`
  - `500 Internal Server Error`: `{"error": "Failed to verify email"}`

---

### 3.3 User Profile (`/api/v1/user`)

#### `GET /api/v1/user/profile`
Retrieves the authenticated user's account details.
- **Auth**: **Required**
- **Go Response Struct** (`types.UserResponse`):
  ```go
  type UserResponse struct {
      ID         uint   `json:"id"`
      Name       string `json:"name"`
      Email      string `json:"email"`
      IsActive   bool   `json:"isActive"`
      IsVerified bool   `json:"isVerified"`
  }
  ```
- **Example Request**:
  ```bash
  curl http://localhost:8080/api/v1/user/profile -b cookies.txt
  ```
- **Responses**:
  - `200 OK`:
    ```json
    {
      "id": 1,
      "name": "Jane Doe",
      "email": "jane@example.com",
      "isActive": true,
      "isVerified": true
    }
    ```
  - `401 Unauthorized`: `{"error": "Unauthorized"}`
  - `500 Internal Server Error`: `{"error": "Failed to retrieve user profile"}`

#### `PUT /api/v1/user/profile`
Updates the authenticated user's name and/or email address.
- **Auth**: **Required**
- **Go Request Struct** (`types.UserRequest`):
  ```go
  type UserRequest struct {
      Name  string `json:"name"`
      Email string `json:"email"`
  }
  ```
- **Example Request**:
  ```bash
  curl -X PUT http://localhost:8080/api/v1/user/profile \
    -b cookies.txt \
    -H "Content-Type: application/json" \
    -d '{
      "name": "Jane Smith",
      "email": "janesmith@example.com"
    }'
  ```
- **Responses**:
  - `200 OK`: `{"message": "User profile updated successfully"}`
  - `400 Bad Request`: `{"error": "Invalid request body"}`
  - `401 Unauthorized`: `{"error": "Unauthorized"}`
  - `500 Internal Server Error`: `{"error": "Failed to update user profile"}`

---

### 3.4 User Settings (`/api/v1/user/settings`)

Manages per-user application preferences, such as daily flashcard learning volume.

#### `GET /api/v1/user/settings`
Retrieves settings for the authenticated user.
- **Auth**: **Required**
- **Go Response Struct** (`types.SettingsResponse`):
  ```go
  type SettingsResponse struct {
      CardsPerDay uint `json:"cardsPerDay"`
  }
  ```
- **Example Request**:
  ```bash
  curl http://localhost:8080/api/v1/user/settings -b cookies.txt
  ```
- **Responses**:
  - `200 OK`:
    ```json
    {
      "cardsPerDay": 20
    }
    ```
  - `401 Unauthorized`: `{"error": "Unauthorized"}`
  - `500 Internal Server Error`: `{"error": "Failed to retrieve settings"}`

#### `PUT /api/v1/user/settings`
Updates settings for the authenticated user.
- **Auth**: **Required**
- **Go Request Struct** (`types.SettingsResponse`):
  ```go
  type SettingsResponse struct {
      CardsPerDay uint `json:"cardsPerDay"`
  }
  ```
- **Example Request**:
  ```bash
  curl -X PUT http://localhost:8080/api/v1/user/settings \
    -b cookies.txt \
    -H "Content-Type: application/json" \
    -d '{
      "cardsPerDay": 30
    }'
  ```
- **Responses**:
  - `202 Accepted`: `{"message": "Settings updated"}`
  - `400 Bad Request`: `{"error": "Invalid request body"}`
  - `401 Unauthorized`: `{"error": "Unauthorized"}`
  - `500 Internal Server Error`: `{"error": "Failed to update settings"}`

---

### 3.5 Decks (`/api/v1/decks`)

Supported deck types are `"vocabulary"` and `"grammar"`.

#### `GET /api/v1/decks/`
Lists all active decks.
- **Auth**: **Required**
- **Go Response Struct** (`[]types.Deck`):
  ```go
  type Deck struct {
      ID   uint   `json:"id"`
      Name string `json:"name"`
      Type string `json:"type"` // "vocabulary" | "grammar"
  }
  ```
- **Example Request**:
  ```bash
  curl http://localhost:8080/api/v1/decks/ -b cookies.txt
  ```
- **Response `200 OK`**:
  ```json
  [
    {
      "id": 1,
      "name": "TOPIK I Vocabulary",
      "type": "vocabulary"
    },
    {
      "id": 2,
      "name": "Basic Verb Endings",
      "type": "grammar"
    }
  ]
  ```

#### `POST /api/v1/decks/`
Creates a new deck.
- **Auth**: **Required**
- **Go Request Struct** (`types.DeckRequest`):
  ```go
  type DeckRequest struct {
      Name string `json:"name"`
      Type string `json:"type"` // "vocabulary" | "grammar"
  }
  ```
- **Example Request**:
  ```bash
  curl -X POST http://localhost:8080/api/v1/decks/ \
    -b cookies.txt \
    -H "Content-Type: application/json" \
    -d '{
      "name": "TOPIK II Advanced Vocab",
      "type": "vocabulary"
    }'
  ```
- **Responses**:
  - `201 Created`: `{"message": "Deck created"}`
  - `400 Bad Request`: `{"error": "Invalid request body"}`
  - `500 Internal Server Error`: `{"error": "Failed to create Deck"}`

#### `GET /api/v1/decks/:id`
Retrieves a single deck by its ID.
- **Auth**: **Required**
- **Path Parameter**: `:id` (integer)
- **Response `200 OK`** (`types.Deck`):
  ```json
  {
    "id": 1,
    "name": "TOPIK I Vocabulary",
    "type": "vocabulary"
  }
  ```

#### `PUT /api/v1/decks/:id`
Updates an existing deck.
- **Auth**: **Required**
- **Path Parameter**: `:id` (integer)
- **Go Request Struct** (`types.DeckRequest`):
  ```json
  {
    "name": "TOPIK I Vocabulary (Updated)",
    "type": "vocabulary"
  }
  ```
- **Responses**:
  - `202 Accepted`: `{"message": "Deck updated"}`
  - `400 Bad Request`: `{"error": "Invalid request body"}`
  - `500 Internal Server Error`: `{"error": "Failed to update Deck"}`

#### `DELETE /api/v1/decks/:id`
Soft-deletes a deck.
- **Auth**: **Required**
- **Path Parameter**: `:id` (integer)
- **Responses**:
  - `202 Accepted`: `{"message": "Deck deleted"}`
  - `500 Internal Server Error`: `{"error": "Failed to delete Deck"}`

---

### 3.6 Cards (`/api/v1/cards`)

Cards represent individual vocabulary or grammar flashcards belonging to a deck.

#### `GET /api/v1/cards/`
Lists all active cards.
- **Auth**: **Required**
- **Go Response Struct** (`[]types.Card`):
  ```go
  type Card struct {
      ID          uint   `json:"id"`
      DeckID      uint   `json:"deckId"`
      KoreanWord  string `json:"koreanWord"`
      EnglishWord string `json:"englishWord"`
      Context     string `json:"context"`
      Example     string `json:"example"`
  }
  ```
- **Example Request**:
  ```bash
  curl http://localhost:8080/api/v1/cards/ -b cookies.txt
  ```
- **Response `200 OK`**:
  ```json
  [
    {
      "id": 1,
      "deckId": 1,
      "koreanWord": "선생님",
      "englishWord": "Teacher",
      "context": "Noun / Honorific",
      "example": "선생님, 질문이 있습니다."
    }
  ]
  ```

#### `POST /api/v1/cards/`
Creates a new card in a specified deck.
- **Auth**: **Required**
- **Go Request Struct** (`types.CardRequest`):
  ```go
  type CardRequest struct {
      DeckID      uint   `json:"deckId"`
      KoreanWord  string `json:"koreanWord"`
      EnglishWord string `json:"englishWord"`
      Context     string `json:"context"`
      Example     string `json:"example"`
  }
  ```
- **Example Request**:
  ```bash
  curl -X POST http://localhost:8080/api/v1/cards/ \
    -b cookies.txt \
    -H "Content-Type: application/json" \
    -d '{
      "deckId": 1,
      "koreanWord": "도서관",
      "englishWord": "Library",
      "context": "Place / Noun",
      "example": "도서관에서 책을 읽어요."
    }'
  ```
- **Responses**:
  - `201 Created`: `{"message": "Card created"}`
  - `400 Bad Request`: `{"error": "Invalid request body"}`
  - `500 Internal Server Error`: `{"error": "Failed to create Card"}`

#### `GET /api/v1/cards/:id`
Retrieves a card by ID.
- **Auth**: **Required**
- **Path Parameter**: `:id` (integer)
- **Response `200 OK`** (`types.Card`):
  ```json
  {
    "id": 1,
    "deckId": 1,
    "koreanWord": "도서관",
    "englishWord": "Library",
    "context": "Place / Noun",
    "example": "도서관에서 책을 읽어요."
  }
  ```

#### `PUT /api/v1/cards/:id`
Updates card details.
- **Auth**: **Required**
- **Path Parameter**: `:id` (integer)
- **Go Request Struct** (`types.CardRequest`):
  ```json
  {
    "deckId": 1,
    "koreanWord": "도서관",
    "englishWord": "Library",
    "context": "Place / Public Institution",
    "example": "주말에는 도서관에 사람이 많습니다."
  }
  ```
- **Responses**:
  - `202 Accepted`: `{"message": "Card updated"}`
  - `400 Bad Request`: `{"error": "Invalid request body"}`
  - `500 Internal Server Error`: `{"error": "Failed to update Card"}`

#### `DELETE /api/v1/cards/:id`
Soft-deletes a card.
- **Auth**: **Required**
- **Path Parameter**: `:id` (integer)
- **Responses**:
  - `202 Accepted`: `{"message": "Card deleted"}`
  - `500 Internal Server Error`: `{"error": "Failed to delete Card"}`

---

### 3.7 Reviews & Spaced Repetition (`/api/v1/reviews`)

Implements the SuperMemo SM-2 spaced repetition algorithm for cards assigned to the current user.

#### `POST /api/v1/reviews/`
Associates a card with the authenticated user's study deck (`user_cards` table) with default initial factors (`efactor: 2.5`, `interval: 1`).
- **Auth**: **Required**
- **Go Request Struct** (`types.Review`):
  ```go
  type Review struct {
      CardID uint `json:"cardID"` // Note uppercase ID in JSON key
  }
  ```
- **Example Request**:
  ```bash
  curl -X POST http://localhost:8080/api/v1/reviews/ \
    -b cookies.txt \
    -H "Content-Type: application/json" \
    -d '{
      "cardID": 1
    }'
  ```
- **Responses**:
  - `201 Created`: `{"message": "Review created"}`
  - `400 Bad Request`: `{"error": "Invalid request body"}` or `{"error": "Failed to create Review"}`
  - `401 Unauthorized`: `{"error": "Unauthorized"}`

#### `GET /api/v1/reviews/`
Retrieves all cards currently due for review (`next_review_at <= NOW()`) for the authenticated user.
- **Auth**: **Required**
- **Response `200 OK`**: Array of `types.Card` objects:
  ```json
  [
    {
      "id": 1,
      "deckId": 1,
      "koreanWord": "도서관",
      "englishWord": "Library",
      "context": "Place / Noun",
      "example": "도서관에서 책을 읽어요."
    }
  ]
  ```

#### `PUT /api/v1/reviews/:id`
Submits a rating score (0–5) after reviewing a card. The server recalculates the SM-2 ease factor (`EF`), repetition interval, and schedules the `next_review_at` date.
- **Auth**: **Required**
- **Path Parameter**: `:id` (Card ID, integer)
- **Go Request Struct** (`types.ReviewRequest`):
  ```go
  type ReviewRequest struct {
      Ease int `json:"ease"` // Score 0 through 5
  }
  ```
- **SM-2 Ease Scale**:
  - `0`: Complete blackout / forgotten
  - `1`: Incorrect response; correct answer remembered upon seeing it
  - `2`: Incorrect response; seemed easy to recall
  - `3`: Correct response, but with serious difficulty
  - `4`: Correct response after hesitation
  - `5`: Perfect response / instant recall
- **Example Request**:
  ```bash
  curl -X PUT http://localhost:8080/api/v1/reviews/1 \
    -b cookies.txt \
    -H "Content-Type: application/json" \
    -d '{
      "ease": 4
    }'
  ```
- **Responses**:
  - `200 OK`: `{"message": "Review updated"}`
  - `400 Bad Request`: `{"error": "Invalid request body"}`
  - `500 Internal Server Error`: `{"error": "Failed to update UserCard"}`

#### `GET /api/v1/reviews/since/:time`
Fetches cards due for review within a given time window (`next_review_at <= NOW() AND next_review_at > time`).
- **Auth**: **Required**
- **Path Parameter**: `:time` (RFC3339 formatted date/time string, URL-encoded)
- **Example Request**:
  ```bash
  curl "http://localhost:8080/api/v1/reviews/since/2026-10-01T00:00:00Z" -b cookies.txt
  ```
- **Response `200 OK`**: Array of `types.Card` objects

---

## 4. Quick Route Summary

| Method | Endpoint | Auth | Request Body | Response Body |
| :--- | :--- | :---: | :--- | :--- |
| `GET` | `/healthz` | No | None | `{"status":"ok"}` |
| `GET` | `/readyz` | No | None | `{"status":"ok"}` |
| `POST` | `/api/v1/register` | No | `RegisterRequestBody` | `{"message":"..."}` |
| `POST` | `/api/v1/login` | No | `LoginRequestBody` | `{"message":"..."}` |
| `POST` | `/api/v1/logout` | **Yes** | None | `{"message":"..."}` |
| `POST` | `/api/v1/verification/:token` | No | None | `{"message":"..."}` |
| `GET` | `/api/v1/user/profile` | **Yes** | None | `UserResponse` |
| `PUT` | `/api/v1/user/profile` | **Yes** | `UserRequest` | `{"message":"..."}` |
| `GET` | `/api/v1/user/settings` | **Yes** | None | `SettingsResponse` |
| `PUT` | `/api/v1/user/settings` | **Yes** | `SettingsResponse` | `{"message":"..."}` |
| `GET` | `/api/v1/decks/` | **Yes** | None | `[]Deck` |
| `POST` | `/api/v1/decks/` | **Yes** | `DeckRequest` | `{"message":"..."}` |
| `GET` | `/api/v1/decks/:id` | **Yes** | None | `Deck` |
| `PUT` | `/api/v1/decks/:id` | **Yes** | `DeckRequest` | `{"message":"..."}` |
| `DELETE`| `/api/v1/decks/:id` | **Yes** | None | `{"message":"..."}` |
| `GET` | `/api/v1/cards/` | **Yes** | None | `[]Card` |
| `POST` | `/api/v1/cards/` | **Yes** | `CardRequest` | `{"message":"..."}` |
| `GET` | `/api/v1/cards/:id` | **Yes** | None | `Card` |
| `PUT` | `/api/v1/cards/:id` | **Yes** | `CardRequest` | `{"message":"..."}` |
| `DELETE`| `/api/v1/cards/:id` | **Yes** | None | `{"message":"..."}` |
| `GET` | `/api/v1/reviews/` | **Yes** | None | `[]Card` |
| `POST` | `/api/v1/reviews/` | **Yes** | `Review` (`{"cardID": uint}`) | `{"message":"..."}` |
| `PUT` | `/api/v1/reviews/:id` | **Yes** | `ReviewRequest` (`{"ease": int}`) | `{"message":"..."}` |
| `GET` | `/api/v1/reviews/since/:time` | **Yes** | None | `[]Card` |

---

## 5. Developer & Implementation Notes

1. **Session User ID Handling**:
   - User IDs are stored as strings in Redis sessions (`fmt.Sprint(user.ID)`).
   - Protected handlers parse this back into an integer using `strconv.ParseInt(userID.(string), 10, 64)` to interact with the database queries.
2. **Review Scheduling (SM-2)**:
   - When a review score (`ease`) is submitted to `PUT /api/v1/reviews/:id`, the card's `efactor`, `interval`, and `next_review_at` timestamps are dynamically calculated based on the SuperMemo SM-2 algorithm.
   - The minimum E-factor is clamped at `1.3`.
   - New intervals advance from 1 day (first interval), to 6 days (second repetition), and subsequently scaled by `newInterval = int32(float64(interval) * newEfactor)`.
