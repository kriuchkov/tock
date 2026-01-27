# Description

<!-- Describe what this PR does. What problem does it solve or what feature does it add? -->

## Type of change

- [ ] Refactoring / Optimization
- [ ] Bug fix
- [ ] New feature
- [ ] Documentation update

---

## Pre-submission Checklist

Please check your code against the following points before creating the PR:

### 1. Code Quality and Simplicity

- [ ] **Data Types:** I used the most appropriate data types (e.g., `time.Duration` for configs instead of `string`) to leverage library capabilities (like Viper's auto-unmarshaling) and avoid redundant parsing methods.
- [ ] **Cleanliness:** Removed redundant comments. The code is self-documenting; comments explain *why*, not *what*, and are only used where logic is not immediately obvious.
- [ ] **Linters:** Code passes linter checks.

### 2. UI / UX (if applicable)

- [ ] **Responsiveness:** UI elements (e.g., progress bars) utilize the full available width of their container/context rather than using hardcoded values.
- [ ] **Display:** Verified correct rendering across different terminal sizes.

### 3. Documentation and Tests

- [ ] **README:** If new configuration options were added, I updated `README.md` and any example configuration files.

---

### Screenshots (for UI changes)
<!-- If the UI has changed, please attach "Before" and "After" screenshots -->
