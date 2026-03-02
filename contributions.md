# Contributing to UGEM

> First off, thank you for considering contributing to UGEM! 🚀

UGEM is a solo-founded project with an ambitious goal: to redefine software execution through deterministic goal resolution. We are building a new computational substrate, and your help is vital to making it production-ready.

---

## 👋 Getting Started

### 1. Explore the Vision

Before diving into the code, please read our [README.md](readme.md) and the UGEM Manifesto. Understanding the shift from "Procedures" to "Goals" is essential for contributing meaningful code.

### 2. Find Something to Work On

| Channel | Description |
|---------|-------------|
| **Issues** | Check the repository for "good first issue" labels |
| **Discussions** | If you have a new idea for the Goal Definition Language (GDL), start a thread in GitHub Discussions |
| **Developer Preview** | We are currently stress-testing. If you find a race condition or a non-deterministic edge case, that is a high-priority contribution! |

---

## ⚖️ Licensing & Contribution Agreement

> By contributing to UGEM, you agree that your contributions will be licensed according to our Split-License Model:

| Contribution Type | License |
|-------------------|---------|
| Substrate Contributions (`/kernel`, `/runtime`, `/planning`, etc.) | **AGPL v3** |
| Surface Contributions (`/gdl`, `/plugins`, `/sdk`) | **MIT** |

See [license_summary.md](license_summary.md) for a full breakdown.

---

## 🛠 Your Contribution Workflow

1. **Fork** the repository and create your branch from `main`

2. **Code** — Ensure your code follows the [Code Standards](code-standards.md)

3. **Test** — We do not accept PRs without accompanying tests. Run `go test ./...` to ensure everything is green

4. **Commit** — Use Conventional Commits

   ```
   Example: feat(planning): add heuristic for multi-step goal resolution
   ```

5. **Submit** — Open a Pull Request with a clear description of the changes and link any relevant issues

---

## 🎯 The "Golden Rules" of UGEM

> If you are contributing to the Substrate, you must adhere to these non-negotiables:

| Rule | Description |
|------|-------------|
| **Strict Determinism** | Logic inside the kernel must be pure. No `time.Now()`, no `rand.Int()`, and no direct network I/O inside state-transition functions. All non-deterministic inputs must be injected via Events. |
| **Error Clarity** | UGEM is a "Safety-First" system. Never ignore an error. Never use `panic()` in production logic. |
| **Readability over Cleverness** | We build infrastructure that others will rely on for decades. Keep it boring, keep it clear. |

---

## 💬 Community & Communication

| Channel | Contact |
|---------|---------|
| **GitHub Discussions** | For architectural debates and GDL proposals |
| **Security** | If you find a vulnerability, please do not open a public issue. Email us at security@ugem-io |

> **Solo Developer Note:** As the primary maintainer, I will review all PRs as quickly as possible. Please be patient as we build this foundation together!

---

## 🏅 Recognition

Every contributor will be added to our `CONTRIBUTORS.md` file. Whether you fix a typo in the docs or optimize the planning algorithm, you are a part of the UGEM Community.

---

<div align="center">

**Let's build the future of software, one goal at a time.**

</div>
