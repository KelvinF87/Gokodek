# Gokodek Manual Técnico y Guía de Configuración

**Gokodek** es un agente de desarrollo local autónomo y altamente optimizado diseñado para funcionar en terminales interactivas (TUI) con modelos locales (Ollama) o remotos (OpenAI, Gemini, OpenRouter).

**Autor**: Kelvin Jose Familia Adames ([KelvinF87](https://kelvinf87.github.io/yo/)) | **4K Services**: [https://4kservices.es/](https://4kservices.es/)  
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

## 🚀 Instalación y Compilación

Gokodek es un binario autónomo escrito en Go. Puedes instalarlo utilizando scripts automatizados, compilando desde el código fuente o descargando los ejecutables precompilados.

### Requisitos Previos Comunes
- **Ollama** (para modelos locales de LLM y RAG vectoriales como `qwen2.5-coder` y `nomic-embed-text`).
- **Navegador Chrome o Edge** (para las herramientas de captura de pantalla y automatización browser_control).

---

### 🪟 Instalación en Windows

#### Opción 1: Script de Instalación Automatizado (Recomendado)
Abre PowerShell como administrador y ejecuta el siguiente comando para descargar e instalar Gokodek automáticamente en tu Path:
```powershell
Set-ExecutionPolicy Bypass -Scope Process -Force; [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.SecurityProtocolType]::Tls12; iex ((New-Object System.Net.WebClient).DownloadString('https://raw.githubusercontent.com/KelvinF87/Gokodek/main/install.ps1'))
```

#### Opción 2: Compilación Manual desde el Código Fuente
Si deseas compilar la última versión de desarrollo, asegúrate de tener instalado **Go 1.22+**:
1. Clona el repositorio:
   ```powershell
   git clone https://github.com/KelvinF87/Gokodek.git
   cd gokodek
   ```
2. Ejecuta el script de compilación para Windows:
   ```powershell
   .\build-windows.ps1
   ```
3. El ejecutable compilado estará disponible en `dist/gokodek.exe`. Añade esta carpeta a tus Variables de Entorno (PATH).

#### Opción 3: Instalación de Binarios Precompilados
1. Descarga el archivo ejecutable `gokodek.exe` directamente desde la sección de **Releases** en GitHub.
2. Mueve el archivo a una carpeta de tu preferencia (ej. `C:\tools\gokodek`).
3. Añade dicha ruta a la variable de entorno `PATH` de tu sistema para ejecutar `gokodek` desde cualquier terminal.

---

### 🐧 Instalación en Linux

#### Opción 1: Instalador Automatizado por Consola
Usa el instalador oficial de un solo comando en tu terminal para descargar, configurar y registrar Gokodek:
```bash
curl -fsSL https://raw.githubusercontent.com/KelvinF87/Gokodek/main/install.sh | bash
```

#### Opción 2: Compilación Manual desde el Código Fuente
1. Asegúrate de tener **Go 1.22+** instalado.
2. Descarga y accede al repositorio:
   ```bash
   git clone https://github.com/KelvinF87/Gokodek.git
   cd gokodek
   ```
3. Otorga permisos y ejecuta el script local de instalación:
   ```bash
   chmod +x install.sh
   ./install.sh
   ```
   *Esto compilará el código y moverá el binario resultante a `/usr/local/bin` o `~/.local/bin`.*

#### Opción 3: Descarga y Registro del Binario Manual
1. Descarga el binario de Linux correspondiente a tu arquitectura (`amd64` o `arm64`) de GitHub Releases.
2. Cámbiale el nombre a `gokodek` y otórgale permisos de ejecución:
   ```bash
   chmod +x gokodek-linux-amd64
   mv gokodek-linux-amd64 gokodek
   ```
3. Muévelo a tu ruta del sistema:
   ```bash
   sudo mv gokodek /usr/local/bin/
   ```

---

### 🍎 Instalación en macOS

#### Opción 1: Instalador Automatizado por Terminal
Instala Gokodek descargando y ejecutando el script oficial mediante curl:
```bash
curl -fsSL https://raw.githubusercontent.com/KelvinF87/Gokodek/main/install.sh | bash
```

#### Opción 2: Compilación y Configuración Local
1. Instala Go (vía Homebrew: `brew install go`).
2. Clona el proyecto y compila:
   ```bash
   git clone https://github.com/KelvinF87/Gokodek.git
   cd gokodek
   go build -o dist/gokodek .
   ```
3. Copia el binario a tu Path local:
   ```bash
   sudo cp dist/gokodek /usr/local/bin/
   ```

#### Opción 3: Instalación de Binarios Precompilados (Apple Silicon & Intel)
1. Descarga el ejecutable para macOS (`mac-arm64` para procesadores M1/M2/M3 o `mac-amd64` para procesadores Intel) desde GitHub Releases.
2. Desbloquea el binario descargado para evitar alertas de Gatekeeper en macOS:
   ```bash
   xattr -d com.apple.quarantine gokodek-mac-arm64
   ```
3. Renómbralo y muévelo a tu directorio binario:
   ```bash
   chmod +x gokodek-mac-arm64
   mv gokodek-mac-arm64 /usr/local/bin/gokodek
   ```

---

## ⚙️ Configuración Inicial y Modelos

### 1. Configurar Modelos Locales (Ollama)
Si usas modelos locales, descarga el modelo de programación y el modelo de embeddings para RAG:
```bash
# Descargar modelo de código
ollama pull qwen2.5-coder:3b

# Descargar modelo RAG para indexación semántica
ollama pull nomic-embed-text
```

### 2. Configurar APIs de Modelos Remotos y MCP
Para configurar tus llaves API (OpenAI, Gemini, OpenRouter) o el endpoint personalizado de Ollama, arranca Gokodek y ejecuta:
```bash
/config
```
O edita directamente tu archivo de perfil global en `~/.gokodek/config.json`.

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
