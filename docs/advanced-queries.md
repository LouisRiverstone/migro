---
title: Advanced queries
nav_exclude: true
description: "This page moved: where variants, unions, locks and full-text search are in Query builder; relation conditions and aggregates in Relations; pagination has its own page."
---

# Advanced queries

This page was split to follow Laravel's documentation:

| Topic | Now in |
|---|---|
| Where clauses (column, like, date, JSON, raw), subqueries | [Query builder](query-builder.md#basic-where-clauses) |
| Full-text search | [Query builder](query-builder.md#full-text-where-clauses) |
| Grouping, unions | [Query builder](query-builder.md#ordering-grouping-limit-and-offset), [Unions](query-builder.md#unions) |
| Locks | [Query builder](query-builder.md#pessimistic-locking) |
| Single records (`Sole`, `FirstWhere`) | [Query builder](query-builder.md#retrieving-a-single-row-or-column-from-a-table) |
| Pagination, cursor pagination | [Pagination](pagination.md) |
| Streaming (`Cursor`, `Lazy`, `Each`, `ChunkByID`) | [Query builder](query-builder.md#chunking-results) |
| Relation conditions (`WhereRelation`, `WhereBelongsTo`, `WhereHasMorph`) | [Relations](relations.md#querying-relations) |
| Relation aggregates (`WithCount`, `WithSum`...) | [Relations](relations.md#aggregating-related-models) |
| `ToSQL` | [Query builder](query-builder.md#debugging) |

### Locks

See [Pessimistic locking](query-builder.md#pessimistic-locking).
