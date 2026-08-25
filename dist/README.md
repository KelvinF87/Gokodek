# gokodek

CLI agéntico ligero en Go para desarrollar proyectos completos con modelos locales de [Ollama](https://ollama.com/). No usa dependencias externas de Go. Recomendamos `qwen3-vl:2b` como modelo Transformer/VLM pequeño para visión: pesa alrededor de 1.9 GB en Ollama, soporta imágenes y aparece con soporte de herramientas. Para instalarlo: `ollama pull qwen3-vl:2b`. Para `browser_screenshot` necesitas tener Chrome, Chromium o Edge instalado; la captura se realiza mediante el ejecutable del navegador y se guarda dentro del workspace. Por defecto también se abre una ventana visible aislada para que puedas observar la prueba; usa `-browser-visible=false` para modo headless. La herramienta incluye un resumen objetivo del DOM (título y texto visible) junto con la imagen para reducir alucinaciones del modelo de visión; aun así, el modelo puede equivocarse y sus afirmaciones se tratan como no confiables.

## Instalar en Windows sin Go

Go solo es necesario en la máquina que compila el release. El equipo destino recibe únicamente `gokodek.exe`.

En la máquina de desarrollo, con Go instalado, ejecuta el flujo completo:

```powershell
.\build-release.ps1
```

También puedes hacer doble clic en `build-release.cmd`. El script primero crea el ejecutable y comprueba que exista; después invoca Inno Setup si está instalado.

Esto crea:

```text
dist\gokodek.exe
```

Para generar un instalador gráfico `.exe`, instala Inno Setup 6 en la máquina de compilación. El resultado será un instalador gráfico solo si `ISCC.exe` está disponible; de lo contrario, el script dejará igualmente el ejecutable standalone en `dist`.

El instalador resultante será:

```text
dist\gokodek-setup-0.1.0.exe
```

El instalador es por usuario, no requiere permisos de administrador y añade gokodek al `PATH` del usuario. Ollama, los modelos y Chrome/Edge siguen siendo requisitos externos: el instalador no descarga modelos de varios gigabytes ni instala servicios silenciosamente.

Si no tienes Inno Setup, puedes distribuir `dist\gokodek.exe` o usar:

```powershell
.\install.ps1 -SourceDirectory .\dist
```

Después de instalar, abre una nueva PowerShell y ejecuta desde cualquier proyecto:

```powershell
cd D:\MisDesarrollos\pruebaGo
gokodek
```

Para desinstalar la instalación manual:

```powershell
.\uninstall.ps1
```

## Requisitos

- Go 1.22 o superior
- Ollama ejecutándose en `http://localhost:11434`
- Un modelo con soporte de tool calling, por ejemplo:

```bash
ollama pull qwen2.5-coder:3b
```

## Ejecutar

```bash
go run main.go
# o elegir otro modelo/endpoint/workspace
go run main.go -model qwen2.5-coder:7b -workspace . -num-ctx 4096 -think=false

# QA visual con un modelo de visión instalado localmente
go run main.go -model qwen2.5-coder:3b -vision-model qwen3-vl:2b -workspace . -browser-visible=true

# Control explícito de escritorio (solo cuando sea necesario)
go run main.go -model qwen2.5-coder:3b -vision-model TU_MODELO_VISION -allow-ui=true -workspace .
```

Dentro del REPL escribe instrucciones como:

```text
Crea un servidor HTTP sencillo en Go, ejecuta sus tests y corrige cualquier error.
```

Comandos del REPL: `/help`, `/clear` y `/quit`.

Por defecto gokodek usa `think=false`, `num_ctx=4096` y `num_predict=4096` para que modelos pequeños respondan rápido y consuman menos memoria. No se añaden dependencias de UI automáticamente: `ui_recipe` recomienda CSS nativo para páginas estáticas, Bootstrap 5 si conviene usar un CDN, y Tailwind solo cuando ya existe un pipeline Node. La respuesta se muestra en streaming. Puedes ajustar estos valores con `-num-ctx`, `-num-predict` y `-think=true`. Para generar una web completa con varios archivos, usa `-num-predict 8192` si el modelo corta el tool call. El límite de rondas de herramientas por turno se ajusta con `-max-rounds` (12 por defecto); al agotarse, el agente cierra con un resumen del trabajo realizado en lugar de fallar en seco.

## Herramientas web y anti-bucles

- `web_search` consulta DuckDuckGo (endpoint HTML) y devuelve resultados reales con títulos, URLs y fragmentos. Si no hay resultados útiles, indica al modelo cambiar de estrategia en vez de repetir la búsqueda.
- `fetch_url` descarga un archivo http(s) dentro del workspace (límite 20 MB) para vendorizar librerías localmente; evita depender de CDNs o redes al probar la página.
- El bucle detecta cuando el modelo llama a la misma herramienta 3 veces seguidas sin progreso: una vez lo corrige y si persiste termina el turno con resumen parcial.
- Una ronda antes de agotar el presupuesto se avisa al modelo para que cierre con un resumen final real.
- La TUI permite hacer scroll de la conversación con PgUp/PgDn (Inicio/Fin para ir a los extremos, End o PgDown hasta el fondo) para revisar y copiar contenido anterior; el pie muestra el porcentaje de scroll cuando no estás al final.

El agente incluye una instrucción de sistema que obliga a usar herramientas y un reintento automático cuando detecta una petición explícita de crear archivos. Los modelos de 1.7B pueden ser menos fiables con tool calling; si continúa respondiendo con tutoriales, prueba `qwen2.5-coder:3b` o `qwen3:4b`.

## Arquitectura

- `pkg/agent/tool.go`: interfaz `Tool`, registro dinámico y schemas Ollama.
- `pkg/agent/ollama.go`: cliente HTTP nativo de `/api/chat`, con streaming NDJSON y opciones de rendimiento.
- `pkg/agent/agent.go`: bucle usuario → modelo → tool calls → resultados → modelo.
- `pkg/tools/files.go`: `read_file`, `write_file` y `list_dir`.
- `pkg/tools/web.go`: `check_web`, verificador local de referencias CSS/JavaScript y comprobaciones visuales estáticas básicas (body vacío, body oculto y contraste negro sobre negro).
- `pkg/tools/fetch.go`: `fetch_url` descarga archivos http(s) al workspace con límite de tamaño.
- `pkg/tools/browser.go`: captura headless aislada, captura de escritorio y controles UI opt-in.
- `pkg/tools/project.go`: `project_info` y `ui_recipe` para detectar stack y elegir CSS nativo, Bootstrap o Tailwind sin instalar nada automáticamente.
- `pkg/tools/command.go`: `run_cmd` con timeout, workspace y filtros de comandos destructivos.
- `main.go`: composición del CLI y ejemplo de herramienta `get_sys_info`.

Las herramientas de archivos y comandos están limitadas al `-workspace` indicado (por defecto, el directorio actual). `check_web` valida archivos y reglas visuales estáticas, pero no reemplaza abrir la página en un navegador real. `browser_screenshot` abre Chrome/Chromium/Edge en modo aislado y guarda una captura en `.gokodek/screenshots`; si se configura `-vision-model`, la imagen se adjunta al siguiente mensaje de Ollama para que el modelo de visión la analice. Las herramientas se omiten durante la llamada visual por defecto. Modelos como `llava:7b` aceptan imágenes pero no soportan tool calling. `qwen3-vl:2b` sí aparece en Ollama con soporte de visión y herramientas; puedes habilitarlo con `-vision-tools=true`. Para probar un HTML local puedes pedir `browser_screenshot` con `path: "index.html"`. `capture_screen` captura el escritorio de Windows y `mouse_click`/`keyboard_type` controlan la sesión activa, pero estas herramientas requieren `-allow-ui=true` y deben usarse solo con una ventana de prueba. El control de escritorio queda desactivado por defecto. `run_cmd` ejecuta comandos de desarrollo en ese directorio; la política incluida es una barrera de seguridad básica, no un sandbox. Revisa las acciones del agente antes de usarlo en un workspace importante.

## Añadir una herramienta

Implementa los cuatro métodos de `agent.Tool` y regístrala con:

```go
registry.Register(miHerramienta)
```

El registro genera automáticamente el schema JSON que se envía a Ollama y despacha cada `tool_call` por nombre. Durante QA se muestra por separado cuándo se abre el navegador, cuándo se captura la imagen y cuándo responde el modelo de visión. Las respuestas se devuelven como mensajes `role: "tool"` con su `tool_name`, que es el formato esperado por Ollama. El bucle corta lotes repetidos y limita cada turno para evitar esperas infinitas.
