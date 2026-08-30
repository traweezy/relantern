"use client";

import type { IntelligenceSearchFilters, SavedIntelligenceSearch } from "@relantern/domain";
import { useForm } from "@tanstack/react-form";
import type { Route } from "next";
import Link from "next/link";
import type { FormEvent } from "react";
import { memo, useCallback, useMemo, useState } from "react";
import { z } from "zod";
import { parseSavedSearch } from "./contract";

type SavedSearchesProps = Readonly<{
  currentFilters: IntelligenceSearchFilters;
  currentQuery: string;
  initialSearches: readonly SavedIntelligenceSearch[];
}>;

type SavedSearchRowProps = Readonly<{
  onDelete: (savedSearchID: string) => void;
  search: SavedIntelligenceSearch;
}>;

const searchHref = (search: SavedIntelligenceSearch): Route => {
  const parameters = new URLSearchParams({ q: search.query });
  for (const [key, value] of Object.entries(search.filters)) {
    if (value !== undefined && value !== "") {
      parameters.set(key, value);
    }
  }
  return `/search?${parameters.toString()}` as Route;
};

const SavedSearchRowComponent = ({ onDelete, search }: SavedSearchRowProps) => {
  const href = useMemo(() => searchHref(search), [search]);
  const handleDelete = useCallback(() => {
    onDelete(search.id);
  }, [onDelete, search.id]);
  return (
    <li>
      <Link href={href}>
        <strong>{search.name}</strong>
        <small>{search.query}</small>
      </Link>
      <button
        aria-label={`Delete saved search ${search.name}`}
        onClick={handleDelete}
        type="button"
      >
        Delete
      </button>
    </li>
  );
};

const SavedSearchRow = memo<SavedSearchRowProps>(SavedSearchRowComponent);
SavedSearchRow.displayName = "SavedSearchRow";

const readProblem = async (response: Response): Promise<string> => {
  const decoded: unknown = await response.json().catch(() => null);
  return typeof decoded === "object" && decoded !== null && "detail" in decoded
    ? String(decoded.detail)
    : "The saved-search command failed.";
};

const SavedSearchesComponent = ({
  currentFilters,
  currentQuery,
  initialSearches,
}: SavedSearchesProps) => {
  const [searches, setSearches] = useState(initialSearches);
  const [notice, setNotice] = useState("Saved searches keep exact filters and remain private.");
  const form = useForm({
    defaultValues: { name: "" },
    validators: { onSubmit: z.object({ name: z.string().trim().min(1).max(100) }) },
    onSubmit: async ({ value }) => {
      if (currentQuery.length < 2) {
        throw new Error("Run a search before saving it.");
      }
      const response = await fetch("/api/discovery/searches", {
        body: JSON.stringify({ filters: currentFilters, name: value.name, query: currentQuery }),
        headers: { accept: "application/json", "content-type": "application/json" },
        method: "POST",
        signal: AbortSignal.timeout(7_000),
      });
      if (!response.ok) {
        throw new Error(await readProblem(response));
      }
      const saved = parseSavedSearch(await response.json());
      setSearches((current) => [saved, ...current.filter((entry) => entry.id !== saved.id)]);
      form.reset();
      setNotice(`Saved “${saved.name}”.`);
    },
  });

  const handleSubmit = useCallback(
    (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      event.stopPropagation();
      void form.handleSubmit().catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "The search could not be saved.");
      });
    },
    [form],
  );

  const deleteSearch = useCallback((savedSearchID: string) => {
    setNotice("Deleting saved search…");
    void fetch(`/api/discovery/searches/${encodeURIComponent(savedSearchID)}`, {
      method: "DELETE",
      signal: AbortSignal.timeout(7_000),
    })
      .then(async (response) => {
        if (!response.ok) {
          throw new Error(await readProblem(response));
        }
        setSearches((current) => current.filter((entry) => entry.id !== savedSearchID));
        setNotice("Saved search deleted.");
      })
      .catch((error: unknown) => {
        setNotice(
          error instanceof Error ? error.message : "The saved search could not be deleted.",
        );
      });
  }, []);

  return (
    <aside aria-labelledby="saved-searches-title" className="saved-searches">
      <div>
        <p className="eyebrow">Durable shortcuts</p>
        <h2 id="saved-searches-title">Saved searches</h2>
        <p aria-live="polite">{notice}</p>
      </div>
      <form onSubmit={handleSubmit}>
        <form.Field name="name">
          {(field) => (
            <label>
              Name current query
              <input
                aria-invalid={field.state.meta.errors.length > 0}
                disabled={currentQuery.length < 2}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
                placeholder="Weekly database changes"
                value={field.state.value}
              />
            </label>
          )}
        </form.Field>
        <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
          {([canSubmit, isSubmitting]) => (
            <button disabled={!canSubmit || isSubmitting || currentQuery.length < 2} type="submit">
              {isSubmitting ? "Saving…" : "Save query"}
            </button>
          )}
        </form.Subscribe>
      </form>
      {searches.length === 0 ? (
        <p className="saved-search-empty">No saved searches yet.</p>
      ) : (
        <ul>
          {searches.map((search) => (
            <SavedSearchRow key={search.id} onDelete={deleteSearch} search={search} />
          ))}
        </ul>
      )}
    </aside>
  );
};

export const SavedSearches = memo<SavedSearchesProps>(SavedSearchesComponent);
SavedSearches.displayName = "SavedSearches";
