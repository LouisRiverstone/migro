---
title: Pagination
nav_order: 7.5
description: "Paginate, SimplePaginate and CursorPaginate on the table builder and model queries, page objects, building pages by hand, URLs and JSON."
---

# Pagination

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

Paginating in other frameworks can be painful. migro's paginators work on the table builder and on model queries, with no configuration. There are no HTML views: you get a page value to render, or to return as JSON through a [resource](resources.md#pagination).

## Basic usage

### Paginating query builder results

`Paginate` takes the page number (starting at 1) and the page size, runs a `COUNT` and the page query, and returns the page:

```go
page, err := query.Table(db, "users").OrderBy("id", "asc").Paginate(ctx, 1, 15)
page.Data        // []query.Row
page.Total       // rows across every page
page.PerPage     // 15
page.CurrentPage // 1
page.LastPage    // ceil(Total / PerPage)
```

Laravel reads the page number from the `?page=` query string for you. In Go, read it from the request yourself:

```go
n, _ := strconv.Atoi(r.URL.Query().Get("page"))
page, err := orm.New[User](db).Query().OrderBy("id", "asc").Paginate(ctx, max(n, 1), 15)
```

A page below 1 is treated as 1; a page size below 1 is an error. Always order a paginated query: without `OrderBy`, the database may return rows in a different order from one page to the next.

#### Simple pagination

When you only need "previous" and "next" links, skip the `COUNT`. `SimplePaginate` reads one extra row to know whether another page follows:

```go
page, err := orm.New[User](db).Query().OrderBy("id", "asc").SimplePaginate(ctx, 2, 15)
page.Data, page.PerPage, page.CurrentPage
page.HasMore // a next page exists
```

### Paginating model results

Model queries paginate the same way, with conditions, scopes and eager loading:

```go
page, err := orm.New[User](db).Query().
    Where("votes > ?", 100).
    With("Posts").
    OrderBy("id", "asc").
    Paginate(ctx, 1, 15)

for _, user := range page.Data { // orm.Collection[User]
    ...
}
```

| Method | Returns | Queries | Use it for |
|---|---|---|---|
| `Paginate(ctx, page, perPage)` | `orm.Page[T]` | a `COUNT` plus the page | numbered pages with a total |
| `SimplePaginate(ctx, page, perPage)` | `orm.SimplePage[T]` | the page (one extra row) | previous / next links |
| `CursorPaginate(ctx, perPage, cursor)` | `orm.CursorPage[T]` | the page, constant time at any depth | infinite scroll, APIs, large tables |

The table builder has `Paginate` only.

#### Multiple paginators per page

Laravel names the query parameter of each paginator (`pageName`). With migro you read the parameters, so name them as you like: `?users_page=2&orders_page=5`.

### Cursor pagination

Offset pagination asks the database to skip rows: `LIMIT 15 OFFSET 30000` still reads the 30,000 rows it skips. Cursor pagination filters on the position of the last row instead (`WHERE (created_at, id) < (?, ?)`), so every page costs the same, and inserts between requests do not shift the pages.

```go
page, err := orm.New[Post](db).Query().OrderBy("created_at", "desc").CursorPaginate(ctx, 20, "")
page.Data       // the posts
page.NextCursor // pass it back for the next page; "" when there is none
page.PrevCursor // pass it back for the previous page; "" on the first page

next, err := orm.New[Post](db).Query().OrderBy("created_at", "desc").CursorPaginate(ctx, 20, page.NextCursor)
```

Send the cursor to the client as an opaque string (`?cursor=...`) and pass it back unchanged.

#### Cursor vs. offset pagination

Cursor pagination is faster on large tables and stable under concurrent writes, with these limits, the same as Laravel's:

- it gives "next" and "previous" only, with no page numbers and no total;
- the query needs at least one `OrderBy`. migro adds the primary key as a tie-breaker, so the ordering is unique;
- order columns must not be `NULL`;
- a cursor made for a different ordering is refused with `QUERY_FAILED`.

### Manually creating a paginator

To paginate something that is not a query (a slice you already have, an external API), build the page yourself:

```go
func PageOf[T any](items []T, page, perPage int) orm.Page[T] {
    total := len(items)
    from := min((page-1)*perPage, total)
    to := min(from+perPage, total)
    return orm.Page[T]{
        Data: items[from:to], Total: int64(total),
        PerPage: perPage, CurrentPage: page, LastPage: max((total+perPage-1)/perPage, 1),
    }
}
```

As with Laravel's `LengthAwarePaginator`, you slice the items yourself.

### Customizing pagination URLs

`Resource.Paginated` builds Laravel-style links from a base URL. Existing query parameters are kept and only `page` is set:

```go
body, err := UserResource.Paginated(page, "https://api.example.com/users?sort=votes")
// links.next: https://api.example.com/users?page=3&sort=votes
```

That covers Laravel's `withPath` (the base URL) and `appends`/`withQueryString` (the parameters you put in it). Add a `#fragment` to the base URL to keep a fragment.

## Displaying pagination results

There are no Blade-style `links()` views: render `page.Data` and the page numbers with your template engine, or return JSON.

### Converting results to JSON

The page types have no JSON tags, so `json.Marshal(page)` uses Go's field names (`Data`, `Total`, `PerPage`...). For Laravel's JSON shape, wrap the page in a resource:

```go
body, err := UserResource.Paginated(page, r.URL.String())
json.NewEncoder(w).Encode(body)
```

```json
{
  "data": [{ "id": 16, "name": "Ana" }],
  "meta": { "current_page": 2, "last_page": 4, "per_page": 15, "total": 52, "from": 16, "to": 30 },
  "links": {
    "first": "https://api.example.com/users?page=1",
    "last":  "https://api.example.com/users?page=4",
    "prev":  "https://api.example.com/users?page=1",
    "next":  "https://api.example.com/users?page=3"
  }
}
```

`from` and `to` are the positions of the first and last item on the page, both `0` for an empty page. See [Resources](resources.md#pagination).

## Page fields

| Laravel method | `orm.Page[T]` | `orm.SimplePage[T]` | `orm.CursorPage[T]` |
|---|---|---|---|
| `items()` | `Data` | `Data` | `Data` |
| `count()` | `len(Data)` | `len(Data)` | `len(Data)` |
| `currentPage()` | `CurrentPage` | `CurrentPage` | |
| `perPage()` | `PerPage` | `PerPage` | `PerPage` |
| `total()` | `Total` | | |
| `lastPage()` | `LastPage` | | |
| `hasMorePages()` | `CurrentPage < LastPage` | `HasMore` | `NextCursor != ""` |
| `onFirstPage()` | `CurrentPage == 1` | `CurrentPage == 1` | `PrevCursor == ""` |
| `nextCursor()`, `previousCursor()` | | | `NextCursor`, `PrevCursor` |
| `firstItem()`, `lastItem()` | `meta.from`, `meta.to` of `Resource.Paginated` | | |
