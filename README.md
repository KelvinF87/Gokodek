# Gokodek Manual Técnico y Guía de Configuración

**Gokodek** es un agente de desarrollo local autónomo y altamente optimizado diseñado para funcionar en terminales interactivas (TUI) con modelos locales (Ollama) o remotos (OpenAI, Gemini, OpenRouter).

**Autor**: Kelvin Jose Familia Adames ([KelvinF87](https://kelvinf87.github.io/yo/))  
**Licencia**: MIT (Open Source)  
**Repositorio Oficial**: [https://github.com/KelvinF87/Gokodek.git](https://github.com/KelvinF87/Gokodek.git)

---

## 🌟 Características Principales

- **Interfaz TUI Avanzada**: Terminal gráfica interactiva con soporte completo para navegación de cursor (edición posicional de texto), menú de modelos rápidos (`F2`), historial con flechas `↑`/`↓`, scroll con `PgUp`/`PgDn` e indicador visual dinámico sin saturación.
- **Persistencia de Modelo por Defecto**: Recuerda siempre el último modelo o perfil utilizado y guarda la lista de los últimos 4 modelos seleccionados para cambio inmediato.
- **RAG Vectorial Global e Incremental**: Almacenamiento e indexación semántica en tiempo real (`rag_search` / `rag_index`) tanto para fragmentos del proyecto actual como para habilidades universales almacenadas en `~/.gokodek/skills/`.
- **Autonomía y Verificación de Proyectos**: Ejecuta servidores locales, analiza errores de consola y realiza inspecciones visuales con `browser_screenshot` antes de entregar un trabajo.
- **Commits de Git Automáticos**: Genera puntos de control en Git para asegurar la integridad de proyectos nuevos o cambios mayores.

---

## 🚀 Instalación y Compilación

### Requisitos Previos
- **Go 1.22+**
- **Ollama** (para ejecución de modelos locales como `qwen2.5-coder`, `qwen3` o `nomic-embed-text` para RAG)

### Compilación e Instalación Multiplataforma

Gokodek está escrito en Go nativo, lo que permite compilar e instalar binarios autónomos sin dependencias en **Linux**, **macOS** y **Windows**.

#### 🐧 Linux / 🍎 macOS
```bash
curl -fsSL https://raw.githubusercontent.com/KelvinF87/Gokodek/main/install.sh | bash
# O compilando desde el fuente:
./install.sh
```

#### 🪟 Windows (PowerShell)
```powershell
.\install.ps1
```

#### 📦 Compilación Cruzada Multiplataforma (Cross-compilation)
Para compilar los binarios de todas las plataformas desde cualquier sistema operativo:
```bash
# En Bash:
./build-all.sh

# En PowerShell:
.\build-all.ps1
```
*Los ejecutables para Linux (`amd64`/`arm64`), macOS (`amd64`/`arm64`) y Windows se generarán en la carpeta `dist/`.*

---

## 🛠️ Guía de Configuración y Comandos

Al iniciar `gokodek`, puedes interactuar mediante comandos con prefijo `/`:

| Comando | Descripción |
|---|---|
| `/modelo` | Abre un menú interactivo para seleccionar el perfil o modelo activo. |
| `F2` | Menú desplegable instantáneo con los últimos 4 modelos seleccionados. |
| `Tab` | Alterna entre modo **build** (ejecución y escritura) y modo **plan** (inspección segura). |
| `/config` | Abre el menú de configuración general (Ollama URL, API Keys de OpenAI/Gemini/OpenRouter, servidores MCP, etc.). |
| `/new` o `/nueva` | Inicia una nueva conversación borrando el historial de contexto. |
| `/talk <tema>` | Inicia un modo debate entre agentes locales sobre el tema indicado. |
| `Esc ×3` | Cancela la ejecución del agente en curso. |

---

## 📁 Estructura del Almacén Global (`~/.gokodek/`)

- `~/.gokodek/config.json`: Configuración principal de perfiles, modelo activo, API keys y modelos recientes.
- `~/.gokodek/skills/`: Directorio donde se almacenan las **Habilidades Globales Universales** auto-creadas (`SKILL.md`).
- `~/.gokodek/global_rag_index.json`: Índice de vectores RAG para búsqueda semántica global de habilidades.

---

## 📄 Licencia

Este proyecto está distribuido bajo la licencia libre **MIT**. Consulta el archivo `LICENSE` para más detalles.
