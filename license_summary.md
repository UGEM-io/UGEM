# ⚖️ UGEM Licensing Summary

> UGEM is a foundational technology. To ensure that the core engine remains free and open forever while allowing for a commercial ecosystem to flourish on top of it, we use a **split-license model**.

---

## 🏗️ The Split at a Glance

| Component | Directory | License | Why? |
|-----------|-----------|---------|------|
| **The Engine** (kernel, runtime) | `/kernel`, `/runtime`, `/storage`, `/distributed`, `/planning`, `/security`, `/logging`, `/observability` | **AGPL v3** | Protects the "substrate." Ensures the core execution model stays open and improvements are shared. |
| **The Surface** (GDL, plugins, examples) | `/gdl`, `/plugins`, `/examples` | **MIT** | Encourages adoption. Allows you to build proprietary business logic and private integrations. |

---

## 🟢 What You CAN Do (MIT)

> The **Surface** (GDL and Plugins) is licensed under the permissive MIT License. This means you can:

- ✅ **Build Private Agents** — Write GDL files that define your proprietary business processes.
- ✅ **Create Private Plugins** — Build internal connectors for your company's private APIs, databases, or trade secrets.
- ✅ **Commercialize Applications** — Build a for-profit application that uses UGEM as its execution engine without being forced to open-source your application's unique business logic.

---

## 🔵 What Is Protected (AGPL)

> The **Engine** (Kernel and Runtime) is licensed under the GNU Affero General Public License (AGPL) v3. This is a "copyleft" license specifically designed for network-based software.

| Requirement | Description |
|-------------|-------------|
| **Modification & Contribution** | If you modify the UGEM Kernel itself (the scheduler, the event-log logic, or the deterministic runner) and use it to provide a service over a network, you must make those modifications available to the community. |
| **No "Strip-Mining"** | This prevents large cloud providers from taking the UGEM engine, making proprietary improvements, and selling it as a closed-source managed service. |

---

## ❓ Frequently Asked Questions

### 1. "If I use UGEM to run my business, do I have to open-source my code?"

**No.** As long as you are only writing GDL files and implementing Plugin interfaces (which are MIT), your specific business logic remains yours. Only if you modify the code inside the `/kernel` or `/runtime` folders do the AGPL requirements apply to those specific changes.

### 2. "Can I build a commercial SaaS on top of UGEM?"

**Yes.** You can build a SaaS that uses the UGEM runtime to execute goals. You can charge for your service. You only need to share code if you have modified the UGEM engine code itself to make your SaaS work.

### 3. "Why not just use MIT for everything?"

UGEM is a new computational model. We believe the "Operating System" for goal-driven execution should be a common public good. The AGPL ensures that the foundation remains decentralized and that no single entity can "fork and close" the core technology.

---

## 🤝 Summary for Contributors

> By contributing to this project, you agree that:

| Contribution Type | License Applied |
|-------------------|-----------------|
| Contributions to `/kernel` and `/runtime` | **AGPL v3** |
| Contributions to `/gdl` and `/plugins` | **MIT** |

---

*This document provides a high-level overview of the UGEM licensing model. For full legal text, please refer to the individual LICENSE files in each directory.*
