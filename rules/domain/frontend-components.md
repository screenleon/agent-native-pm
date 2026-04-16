# Domain Rules: Frontend Components — Agent Native PM

## Rule entries

### Rule: UI-001
- Owner layer: Domain
- Domain: frontend-components
- Stability: behavior
- Status: active
- Scope: shared UI components
- Statement: Shared components in `frontend/src/components/` must be stateless (props-driven) unless local UI state (e.g., toggle, hover) is required.
- Rationale: Predictable rendering, easier testing, and simpler data flow.
- Verification: Component tests verify prop-driven behavior.
- Supersedes: N/A
- Superseded by: N/A

### Rule: UI-002
- Owner layer: Domain
- Domain: frontend-components
- Stability: behavior
- Status: active
- Scope: data display components
- Statement: All components that fetch data must handle loading, empty, and error states explicitly.
- Rationale: Users and agents need clear feedback; prevents blank screens.
- Verification: Component tests cover all three states.
- Supersedes: N/A
- Superseded by: N/A

### Rule: UI-003
- Owner layer: Domain
- Domain: frontend-components
- Stability: behavior
- Status: active
- Scope: API communication
- Statement: All API calls from the frontend must go through a centralized API client layer (`frontend/src/api/`), not inline `fetch()` calls.
- Rationale: Centralizes error handling, envelope parsing, and base URL configuration.
- Verification: Grep for raw `fetch(` calls outside the API layer.
- Supersedes: N/A
- Superseded by: N/A

### Rule: UI-004
- Owner layer: Domain
- Domain: frontend-components
- Stability: behavior
- Status: active
- Scope: page components
- Statement: Page components (`frontend/src/pages/`) own layout and data fetching. Shared components receive data via props.
- Rationale: Clear separation of concerns; pages orchestrate, components render.
- Verification: Code review for data fetching in shared components.
- Supersedes: N/A
- Superseded by: N/A

### Rule: UI-005
- Owner layer: Domain
- Domain: frontend-components
- Stability: experimental
- Status: active
- Scope: styling
- Statement: Use a consistent styling approach across all components (CSS Modules, Tailwind, or styled-components — to be decided at implementation start). Record the choice in `DECISIONS.md`.
- Rationale: Prevents style fragmentation.
- Verification: No mixed styling approaches in the same module.
- Supersedes: N/A
- Superseded by: N/A

### Rule: UI-006
- Owner layer: Domain
- Domain: frontend-components
- Stability: behavior
- Status: active
- Scope: document registry UI
- Statement: Registered documents with a valid `file_path` must be viewable directly within the PM system UI (in-app preview), without forcing users to switch tools.
- Rationale: Documentation review is a core workflow and should be available at point-of-use during project tracking.
- Verification: Project document list provides an in-app view action and renders content or a clear error state.
- Supersedes: N/A
- Superseded by: N/A
