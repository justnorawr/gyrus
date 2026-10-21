# Retrieval evaluation

`queries.json` is a labelled query set for the noratics-wiki store: each query with the wiki ids a good answer needs. `source: real` queries are prompts from agent-ops's lookup log (`lookup` is the row id at `http://127.0.0.1:7799/lookups`); `source: written` ones are written from the documents' content, phrased away from their titles.

Run it over a store (with Go, or in the `golang:1.25` container):

```
GYRUS_EVAL_WIKI=/path/to/noratics-wiki GYRUS_EVAL_OUT=results.md go test ./tests/eval/ -run TestRetrievalVariants -count=1
```

It scores each tokenizer variant (stopwords dropped or kept, plurals folded or kept): recall@1/3/5 and MRR of the top 10 search results, and the estimated tokens of the 1500-token context layer with the share of expected documents that reach it. A label naming a document the store no longer has fails the run: relabel, then rerun. Results are kept beside it as `results-<date>.md`.
