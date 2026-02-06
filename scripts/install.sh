#!/bin/bash
#
# QuantMesh Installation Script
# Usage: sudo ./install.sh
#
# Features:
#   - Auto-install QuantMesh binary to /opt/quantmesh
#   - Configure systemd service
#   - Handle config files (backup/keep/overwrite)
#   - Create necessary users and directories
#   - Multi-language support (16 languages: English, Simplified Chinese, Traditional Chinese,
#     Spanish, French, Russian, Hindi, Portuguese, German, Korean, Arabic, Turkish,
#     Vietnamese, Italian, Indonesian, Dutch)
#

set -e

# Color definitions
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Configuration
INSTALL_DIR="/opt/quantmesh"
SERVICE_NAME="quantmesh"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
CONFIG_FILE="${INSTALL_DIR}/config.yaml"
BACKUP_DIR="${INSTALL_DIR}/backups"
DATA_DIR="${INSTALL_DIR}/data"
LOGS_DIR="${INSTALL_DIR}/logs"
INSTALLED_VERSION="unknown"

# Language selection
LANG_CODE="en"

# Get script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ============================================================================
# Multi-language Support
# ============================================================================

# Initialize language strings
declare -A LANG_STRINGS

# Language selection menu
select_language() {
    echo ""
    echo "=============================================="
    echo "     QuantMesh Installation Script"
    echo "=============================================="
    echo ""
    echo "Please select your language / 请选择您的语言:"
    echo ""
    echo "  [ 1] English"
    echo "  [ 2] 简体中文 (Simplified Chinese)"
    echo "  [ 3] 繁體中文 (Traditional Chinese)"
    echo "  [ 4] Español (Spanish)"
    echo "  [ 5] Français (French)"
    echo "  [ 6] Русский (Russian)"
    echo "  [ 7] हिन्दी (Hindi)"
    echo "  [ 8] Português (Portuguese)"
    echo "  [ 9] Deutsch (German)"
    echo "  [10] 한국어 (Korean)"
    echo "  [11] العربية (Arabic)"
    echo "  [12] Türkçe (Turkish)"
    echo "  [13] Tiếng Việt (Vietnamese)"
    echo "  [14] Italiano (Italian)"
    echo "  [15] Bahasa Indonesia (Indonesian)"
    echo "  [16] Nederlands (Dutch)"
    echo ""
    
    while true; do
        read -p "Select language [1-16] (default: 1): " lang_choice
        lang_choice=${lang_choice:-1}
        
        case $lang_choice in
            1) LANG_CODE="en"; break ;;
            2) LANG_CODE="zh-CN"; break ;;
            3) LANG_CODE="zh-TW"; break ;;
            4) LANG_CODE="es"; break ;;
            5) LANG_CODE="fr"; break ;;
            6) LANG_CODE="ru"; break ;;
            7) LANG_CODE="hi"; break ;;
            8) LANG_CODE="pt"; break ;;
            9) LANG_CODE="de"; break ;;
            10) LANG_CODE="ko"; break ;;
            11) LANG_CODE="ar"; break ;;
            12) LANG_CODE="tr"; break ;;
            13) LANG_CODE="vi"; break ;;
            14) LANG_CODE="it"; break ;;
            15) LANG_CODE="id"; break ;;
            16) LANG_CODE="nl"; break ;;
            *)
                echo "Invalid choice. Please enter 1-16."
                continue
                ;;
        esac
    done
    
    load_language_strings
    clear
    echo ""
    echo "=============================================="
    echo "     $(t "title")"
    echo "=============================================="
    echo ""
}

# Load language strings
load_language_strings() {
    case $LANG_CODE in
        "zh-CN")
            LANG_STRINGS["title"]="QuantMesh 安装程序"
            LANG_STRINGS["error_root"]="请以 root 权限运行此脚本: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="检测到操作系统: %s %s"
            LANG_STRINGS["error_systemd"]="systemd 不可用。此安装脚本仅支持使用 systemd 的 Linux 系统。"
            LANG_STRINGS["info_systemd"]="systemd 可用"
            LANG_STRINGS["error_arch"]="不支持的架构: %s"
            LANG_STRINGS["error_binary_not_found"]="找不到 QuantMesh 二进制文件。请确保在解压后的目录中运行此脚本。"
            LANG_STRINGS["error_binary_expected"]="期望找到: %s 或 quantmesh"
            LANG_STRINGS["info_binary_found"]="找到二进制文件: %s"
            LANG_STRINGS["warn_config_not_found"]="找不到 config.example.yaml，将跳过配置文件安装"
            LANG_STRINGS["info_config_found"]="找到配置示例: %s"
            LANG_STRINGS["warn_service_not_found"]="找不到 quantmesh.service 模板，将使用内置模板"
            LANG_STRINGS["info_service_found"]="找到服务文件模板: %s"
            LANG_STRINGS["step_create_user"]="创建 quantmesh 用户..."
            LANG_STRINGS["info_user_created"]="用户 quantmesh 已创建"
            LANG_STRINGS["info_user_exists"]="用户 quantmesh 已存在"
            LANG_STRINGS["step_create_dirs"]="创建安装目录..."
            LANG_STRINGS["info_dirs_created"]="目录已创建: %s"
            LANG_STRINGS["step_stop_service"]="停止现有 %s 服务..."
            LANG_STRINGS["info_service_stopped"]="服务已停止"
            LANG_STRINGS["step_install_binary"]="安装二进制文件..."
            LANG_STRINGS["info_binary_installed"]="二进制文件已安装: %s (版本: %s)"
            LANG_STRINGS["step_handle_config"]="处理配置文件..."
            LANG_STRINGS["warn_config_exists"]="检测到已存在的配置文件: %s"
            LANG_STRINGS["config_choice_prompt"]="请选择如何处理："
            LANG_STRINGS["config_choice_1"]="保留现有配置（默认，推荐）"
            LANG_STRINGS["config_choice_2"]="使用新的示例配置覆盖（会备份旧配置）"
            LANG_STRINGS["config_choice_3"]="合并配置（保留旧配置，将示例配置复制为 config.example.yaml）"
            LANG_STRINGS["config_select"]="请选择 [1/2/3] (默认: 1): "
            LANG_STRINGS["info_config_kept"]="保留现有配置文件"
            LANG_STRINGS["info_config_example_copied"]="示例配置已复制到: %s"
            LANG_STRINGS["info_config_backed_up"]="旧配置已备份到: %s"
            LANG_STRINGS["warn_config_updated"]="配置文件已更新，请编辑 %s 配置您的 API 密钥等信息"
            LANG_STRINGS["info_config_merged"]="保留现有配置，复制示例配置供参考"
            LANG_STRINGS["info_invalid_choice"]="无效选择，保留现有配置文件"
            LANG_STRINGS["warn_config_created"]="已创建配置文件: %s"
            LANG_STRINGS["warn_config_edit"]="请编辑配置文件，填入您的 API 密钥和交易参数！"
            LANG_STRINGS["step_install_service"]="安装 systemd 服务..."
            LANG_STRINGS["info_service_installed"]="systemd 服务已安装并启用"
            LANG_STRINGS["step_set_permissions"]="设置文件权限..."
            LANG_STRINGS["info_permissions_set"]="权限已设置"
            LANG_STRINGS["step_copy_scripts"]="复制辅助脚本..."
            LANG_STRINGS["info_scripts_copied"]="辅助脚本已复制"
            LANG_STRINGS["prompt_start_service"]="是否现在启动 QuantMesh 服务？[y/N] (默认: N): "
            LANG_STRINGS["step_start_service"]="启动服务..."
            LANG_STRINGS["info_service_started"]="服务已启动成功"
            LANG_STRINGS["error_service_failed"]="服务启动失败，请检查日志: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="服务未启动。您可以稍后使用以下命令启动："
            LANG_STRINGS["success_title"]="QuantMesh 安装完成！"
            LANG_STRINGS["info_install_dir"]="安装目录: %s"
            LANG_STRINGS["info_config_file"]="配置文件: %s"
            LANG_STRINGS["info_data_dir"]="数据目录: %s"
            LANG_STRINGS["info_backup_dir"]="备份目录: %s"
            LANG_STRINGS["info_logs_dir"]="日志目录: %s"
            LANG_STRINGS["common_commands"]="常用命令："
            LANG_STRINGS["cmd_start"]="启动服务:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="停止服务:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="重启服务:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="查看状态:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="查看日志:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="Web 界面:    http://localhost:28888"
            LANG_STRINGS["important_note"]="重要提示："
            LANG_STRINGS["note_edit_config"]="请先编辑配置文件，填入您的交易所 API 密钥："
            LANG_STRINGS["step_start_install"]="开始安装..."
            ;;
        "zh-TW")
            LANG_STRINGS["title"]="QuantMesh 安裝程式"
            LANG_STRINGS["error_root"]="請以 root 權限執行此腳本: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="偵測到作業系統: %s %s"
            LANG_STRINGS["error_systemd"]="systemd 不可用。此安裝腳本僅支援使用 systemd 的 Linux 系統。"
            LANG_STRINGS["info_systemd"]="systemd 可用"
            LANG_STRINGS["error_arch"]="不支援的架構: %s"
            LANG_STRINGS["error_binary_not_found"]="找不到 QuantMesh 二進位檔案。請確保在解壓後的目錄中執行此腳本。"
            LANG_STRINGS["error_binary_expected"]="期望找到: %s 或 quantmesh"
            LANG_STRINGS["info_binary_found"]="找到二進位檔案: %s"
            LANG_STRINGS["warn_config_not_found"]="找不到 config.example.yaml，將跳過配置檔案安裝"
            LANG_STRINGS["info_config_found"]="找到配置範例: %s"
            LANG_STRINGS["warn_service_not_found"]="找不到 quantmesh.service 範本，將使用內建範本"
            LANG_STRINGS["info_service_found"]="找到服務檔案範本: %s"
            LANG_STRINGS["step_create_user"]="建立 quantmesh 使用者..."
            LANG_STRINGS["info_user_created"]="使用者 quantmesh 已建立"
            LANG_STRINGS["info_user_exists"]="使用者 quantmesh 已存在"
            LANG_STRINGS["step_create_dirs"]="建立安裝目錄..."
            LANG_STRINGS["info_dirs_created"]="目錄已建立: %s"
            LANG_STRINGS["step_stop_service"]="停止現有 %s 服務..."
            LANG_STRINGS["info_service_stopped"]="服務已停止"
            LANG_STRINGS["step_install_binary"]="安裝二進位檔案..."
            LANG_STRINGS["info_binary_installed"]="二進位檔案已安裝: %s (版本: %s)"
            LANG_STRINGS["step_handle_config"]="處理配置檔案..."
            LANG_STRINGS["warn_config_exists"]="偵測到已存在的配置檔案: %s"
            LANG_STRINGS["config_choice_prompt"]="請選擇如何處理："
            LANG_STRINGS["config_choice_1"]="保留現有配置（預設，推薦）"
            LANG_STRINGS["config_choice_2"]="使用新的範例配置覆蓋（會備份舊配置）"
            LANG_STRINGS["config_choice_3"]="合併配置（保留舊配置，將範例配置複製為 config.example.yaml）"
            LANG_STRINGS["config_select"]="請選擇 [1/2/3] (預設: 1): "
            LANG_STRINGS["info_config_kept"]="保留現有配置檔案"
            LANG_STRINGS["info_config_example_copied"]="範例配置已複製到: %s"
            LANG_STRINGS["info_config_backed_up"]="舊配置已備份到: %s"
            LANG_STRINGS["warn_config_updated"]="配置檔案已更新，請編輯 %s 配置您的 API 金鑰等資訊"
            LANG_STRINGS["info_config_merged"]="保留現有配置，複製範例配置供參考"
            LANG_STRINGS["info_invalid_choice"]="無效選擇，保留現有配置檔案"
            LANG_STRINGS["warn_config_created"]="已建立配置檔案: %s"
            LANG_STRINGS["warn_config_edit"]="請編輯配置檔案，填入您的 API 金鑰和交易參數！"
            LANG_STRINGS["step_install_service"]="安裝 systemd 服務..."
            LANG_STRINGS["info_service_installed"]="systemd 服務已安裝並啟用"
            LANG_STRINGS["step_set_permissions"]="設定檔案權限..."
            LANG_STRINGS["info_permissions_set"]="權限已設定"
            LANG_STRINGS["step_copy_scripts"]="複製輔助腳本..."
            LANG_STRINGS["info_scripts_copied"]="輔助腳本已複製"
            LANG_STRINGS["prompt_start_service"]="是否現在啟動 QuantMesh 服務？[y/N] (預設: N): "
            LANG_STRINGS["step_start_service"]="啟動服務..."
            LANG_STRINGS["info_service_started"]="服務已啟動成功"
            LANG_STRINGS["error_service_failed"]="服務啟動失敗，請檢查日誌: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="服務未啟動。您可以稍後使用以下命令啟動："
            LANG_STRINGS["success_title"]="QuantMesh 安裝完成！"
            LANG_STRINGS["info_install_dir"]="安裝目錄: %s"
            LANG_STRINGS["info_config_file"]="配置檔案: %s"
            LANG_STRINGS["info_data_dir"]="資料目錄: %s"
            LANG_STRINGS["info_backup_dir"]="備份目錄: %s"
            LANG_STRINGS["info_logs_dir"]="日誌目錄: %s"
            LANG_STRINGS["common_commands"]="常用命令："
            LANG_STRINGS["cmd_start"]="啟動服務:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="停止服務:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="重新啟動服務:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="查看狀態:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="查看日誌:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="Web 介面:    http://localhost:28888"
            LANG_STRINGS["important_note"]="重要提示："
            LANG_STRINGS["note_edit_config"]="請先編輯配置檔案，填入您的交易所 API 金鑰："
            LANG_STRINGS["step_start_install"]="開始安裝..."
            ;;
        "es")
            LANG_STRINGS["title"]="Instalador de QuantMesh"
            LANG_STRINGS["error_root"]="Por favor ejecute este script con permisos root: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="Sistema operativo detectado: %s %s"
            LANG_STRINGS["error_systemd"]="systemd no está disponible. Este script de instalación solo es compatible con sistemas Linux que usan systemd."
            LANG_STRINGS["info_systemd"]="systemd está disponible"
            LANG_STRINGS["error_arch"]="Arquitectura no soportada: %s"
            LANG_STRINGS["error_binary_not_found"]="No se encontró el archivo binario de QuantMesh. Asegúrese de ejecutar este script en el directorio extraído."
            LANG_STRINGS["error_binary_expected"]="Se esperaba encontrar: %s o quantmesh"
            LANG_STRINGS["info_binary_found"]="Archivo binario encontrado: %s"
            LANG_STRINGS["warn_config_not_found"]="No se encontró config.example.yaml, se omitirá la instalación del archivo de configuración"
            LANG_STRINGS["info_config_found"]="Ejemplo de configuración encontrado: %s"
            LANG_STRINGS["warn_service_not_found"]="No se encontró la plantilla quantmesh.service, se usará la plantilla incorporada"
            LANG_STRINGS["info_service_found"]="Plantilla de archivo de servicio encontrada: %s"
            LANG_STRINGS["step_create_user"]="Creando usuario quantmesh..."
            LANG_STRINGS["info_user_created"]="Usuario quantmesh creado"
            LANG_STRINGS["info_user_exists"]="El usuario quantmesh ya existe"
            LANG_STRINGS["step_create_dirs"]="Creando directorios de instalación..."
            LANG_STRINGS["info_dirs_created"]="Directorios creados: %s"
            LANG_STRINGS["step_stop_service"]="Deteniendo servicio %s existente..."
            LANG_STRINGS["info_service_stopped"]="Servicio detenido"
            LANG_STRINGS["step_install_binary"]="Instalando archivo binario..."
            LANG_STRINGS["info_binary_installed"]="Archivo binario instalado: %s (versión: %s)"
            LANG_STRINGS["step_handle_config"]="Procesando archivo de configuración..."
            LANG_STRINGS["warn_config_exists"]="Se detectó un archivo de configuración existente: %s"
            LANG_STRINGS["config_choice_prompt"]="Por favor seleccione cómo proceder:"
            LANG_STRINGS["config_choice_1"]="Mantener configuración existente (predeterminado, recomendado)"
            LANG_STRINGS["config_choice_2"]="Sobrescribir con nueva configuración de ejemplo (se hará copia de seguridad de la configuración antigua)"
            LANG_STRINGS["config_choice_3"]="Fusionar configuración (mantener configuración antigua, copiar configuración de ejemplo como config.example.yaml)"
            LANG_STRINGS["config_select"]="Seleccione [1/2/3] (predeterminado: 1): "
            LANG_STRINGS["info_config_kept"]="Configuración existente mantenida"
            LANG_STRINGS["info_config_example_copied"]="Configuración de ejemplo copiada a: %s"
            LANG_STRINGS["info_config_backed_up"]="Configuración antigua respaldada en: %s"
            LANG_STRINGS["warn_config_updated"]="Archivo de configuración actualizado, por favor edite %s para configurar sus claves API, etc."
            LANG_STRINGS["info_config_merged"]="Configuración existente mantenida, configuración de ejemplo copiada para referencia"
            LANG_STRINGS["info_invalid_choice"]="Selección inválida, manteniendo configuración existente"
            LANG_STRINGS["warn_config_created"]="Archivo de configuración creado: %s"
            LANG_STRINGS["warn_config_edit"]="Por favor edite el archivo de configuración e ingrese sus claves API y parámetros de trading."
            LANG_STRINGS["step_install_service"]="Instalando servicio systemd..."
            LANG_STRINGS["info_service_installed"]="Servicio systemd instalado y habilitado"
            LANG_STRINGS["step_set_permissions"]="Estableciendo permisos de archivos..."
            LANG_STRINGS["info_permissions_set"]="Permisos establecidos"
            LANG_STRINGS["step_copy_scripts"]="Copiando scripts auxiliares..."
            LANG_STRINGS["info_scripts_copied"]="Scripts auxiliares copiados"
            LANG_STRINGS["prompt_start_service"]="¿Desea iniciar el servicio QuantMesh ahora? [y/N] (predeterminado: N): "
            LANG_STRINGS["step_start_service"]="Iniciando servicio..."
            LANG_STRINGS["info_service_started"]="Servicio iniciado exitosamente"
            LANG_STRINGS["error_service_failed"]="Error al iniciar el servicio, por favor revise los registros: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="Servicio no iniciado. Puede iniciarlo más tarde con el siguiente comando:"
            LANG_STRINGS["success_title"]="¡Instalación de QuantMesh completada!"
            LANG_STRINGS["info_install_dir"]="Directorio de instalación: %s"
            LANG_STRINGS["info_config_file"]="Archivo de configuración: %s"
            LANG_STRINGS["info_data_dir"]="Directorio de datos: %s"
            LANG_STRINGS["info_backup_dir"]="Directorio de respaldo: %s"
            LANG_STRINGS["info_logs_dir"]="Directorio de registros: %s"
            LANG_STRINGS["common_commands"]="Comandos comunes:"
            LANG_STRINGS["cmd_start"]="Iniciar servicio:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="Detener servicio:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="Reiniciar servicio:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="Ver estado:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="Ver registros:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="Interfaz web:    http://localhost:28888"
            LANG_STRINGS["important_note"]="Nota importante:"
            LANG_STRINGS["note_edit_config"]="Por favor edite primero el archivo de configuración e ingrese sus claves API del exchange:"
            LANG_STRINGS["step_start_install"]="Iniciando instalación..."
            ;;
        "fr")
            LANG_STRINGS["title"]="Installateur QuantMesh"
            LANG_STRINGS["error_root"]="Veuillez exécuter ce script avec les privilèges root: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="Système d'exploitation détecté: %s %s"
            LANG_STRINGS["error_systemd"]="systemd n'est pas disponible. Ce script d'installation ne prend en charge que les systèmes Linux utilisant systemd."
            LANG_STRINGS["info_systemd"]="systemd est disponible"
            LANG_STRINGS["error_arch"]="Architecture non prise en charge: %s"
            LANG_STRINGS["error_binary_not_found"]="Fichier binaire QuantMesh introuvable. Assurez-vous d'exécuter ce script dans le répertoire extrait."
            LANG_STRINGS["error_binary_expected"]="Fichier attendu: %s ou quantmesh"
            LANG_STRINGS["info_binary_found"]="Fichier binaire trouvé: %s"
            LANG_STRINGS["warn_config_not_found"]="config.example.yaml introuvable, l'installation du fichier de configuration sera ignorée"
            LANG_STRINGS["info_config_found"]="Exemple de configuration trouvé: %s"
            LANG_STRINGS["warn_service_not_found"]="Modèle quantmesh.service introuvable, le modèle intégré sera utilisé"
            LANG_STRINGS["info_service_found"]="Modèle de fichier de service trouvé: %s"
            LANG_STRINGS["step_create_user"]="Création de l'utilisateur quantmesh..."
            LANG_STRINGS["info_user_created"]="Utilisateur quantmesh créé"
            LANG_STRINGS["info_user_exists"]="L'utilisateur quantmesh existe déjà"
            LANG_STRINGS["step_create_dirs"]="Création des répertoires d'installation..."
            LANG_STRINGS["info_dirs_created"]="Répertoires créés: %s"
            LANG_STRINGS["step_stop_service"]="Arrêt du service %s existant..."
            LANG_STRINGS["info_service_stopped"]="Service arrêté"
            LANG_STRINGS["step_install_binary"]="Installation du fichier binaire..."
            LANG_STRINGS["info_binary_installed"]="Fichier binaire installé: %s (version: %s)"
            LANG_STRINGS["step_handle_config"]="Traitement du fichier de configuration..."
            LANG_STRINGS["warn_config_exists"]="Fichier de configuration existant détecté: %s"
            LANG_STRINGS["config_choice_prompt"]="Veuillez sélectionner comment procéder:"
            LANG_STRINGS["config_choice_1"]="Conserver la configuration existante (par défaut, recommandé)"
            LANG_STRINGS["config_choice_2"]="Remplacer par la nouvelle configuration d'exemple (la configuration ancienne sera sauvegardée)"
            LANG_STRINGS["config_choice_3"]="Fusionner la configuration (conserver l'ancienne, copier l'exemple comme config.example.yaml)"
            LANG_STRINGS["config_select"]="Sélectionnez [1/2/3] (par défaut: 1): "
            LANG_STRINGS["info_config_kept"]="Configuration existante conservée"
            LANG_STRINGS["info_config_example_copied"]="Configuration d'exemple copiée vers: %s"
            LANG_STRINGS["info_config_backed_up"]="Ancienne configuration sauvegardée dans: %s"
            LANG_STRINGS["warn_config_updated"]="Fichier de configuration mis à jour, veuillez éditer %s pour configurer vos clés API, etc."
            LANG_STRINGS["info_config_merged"]="Configuration existante conservée, configuration d'exemple copiée pour référence"
            LANG_STRINGS["info_invalid_choice"]="Choix invalide, conservation de la configuration existante"
            LANG_STRINGS["warn_config_created"]="Fichier de configuration créé: %s"
            LANG_STRINGS["warn_config_edit"]="Veuillez éditer le fichier de configuration et saisir vos clés API et paramètres de trading."
            LANG_STRINGS["step_install_service"]="Installation du service systemd..."
            LANG_STRINGS["info_service_installed"]="Service systemd installé et activé"
            LANG_STRINGS["step_set_permissions"]="Définition des permissions des fichiers..."
            LANG_STRINGS["info_permissions_set"]="Permissions définies"
            LANG_STRINGS["step_copy_scripts"]="Copie des scripts auxiliaires..."
            LANG_STRINGS["info_scripts_copied"]="Scripts auxiliaires copiés"
            LANG_STRINGS["prompt_start_service"]="Voulez-vous démarrer le service QuantMesh maintenant? [y/N] (par défaut: N): "
            LANG_STRINGS["step_start_service"]="Démarrage du service..."
            LANG_STRINGS["info_service_started"]="Service démarré avec succès"
            LANG_STRINGS["error_service_failed"]="Échec du démarrage du service, veuillez vérifier les journaux: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="Service non démarré. Vous pouvez le démarrer plus tard avec la commande suivante:"
            LANG_STRINGS["success_title"]="Installation de QuantMesh terminée!"
            LANG_STRINGS["info_install_dir"]="Répertoire d'installation: %s"
            LANG_STRINGS["info_config_file"]="Fichier de configuration: %s"
            LANG_STRINGS["info_data_dir"]="Répertoire de données: %s"
            LANG_STRINGS["info_backup_dir"]="Répertoire de sauvegarde: %s"
            LANG_STRINGS["info_logs_dir"]="Répertoire des journaux: %s"
            LANG_STRINGS["common_commands"]="Commandes courantes:"
            LANG_STRINGS["cmd_start"]="Démarrer le service:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="Arrêter le service:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="Redémarrer le service:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="Voir le statut:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="Voir les journaux:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="Interface web:    http://localhost:28888"
            LANG_STRINGS["important_note"]="Note importante:"
            LANG_STRINGS["note_edit_config"]="Veuillez d'abord éditer le fichier de configuration et saisir vos clés API de l'exchange:"
            LANG_STRINGS["step_start_install"]="Démarrage de l'installation..."
            ;;
        "ru")
            LANG_STRINGS["title"]="Установщик QuantMesh"
            LANG_STRINGS["error_root"]="Пожалуйста, запустите этот скрипт с правами root: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="Обнаружена операционная система: %s %s"
            LANG_STRINGS["error_systemd"]="systemd недоступен. Этот скрипт установки поддерживает только системы Linux с systemd."
            LANG_STRINGS["info_systemd"]="systemd доступен"
            LANG_STRINGS["error_arch"]="Неподдерживаемая архитектура: %s"
            LANG_STRINGS["error_binary_not_found"]="Бинарный файл QuantMesh не найден. Убедитесь, что вы запускаете этот скрипт в извлечённом каталоге."
            LANG_STRINGS["error_binary_expected"]="Ожидается: %s или quantmesh"
            LANG_STRINGS["info_binary_found"]="Бинарный файл найден: %s"
            LANG_STRINGS["warn_config_not_found"]="config.example.yaml не найден, установка конфигурации будет пропущена"
            LANG_STRINGS["info_config_found"]="Найден пример конфигурации: %s"
            LANG_STRINGS["warn_service_not_found"]="Шаблон quantmesh.service не найден, будет использован встроенный шаблон"
            LANG_STRINGS["info_service_found"]="Шаблон файла службы найден: %s"
            LANG_STRINGS["step_create_user"]="Создание пользователя quantmesh..."
            LANG_STRINGS["info_user_created"]="Пользователь quantmesh создан"
            LANG_STRINGS["info_user_exists"]="Пользователь quantmesh уже существует"
            LANG_STRINGS["step_create_dirs"]="Создание каталогов установки..."
            LANG_STRINGS["info_dirs_created"]="Каталоги созданы: %s"
            LANG_STRINGS["step_stop_service"]="Остановка существующей службы %s..."
            LANG_STRINGS["info_service_stopped"]="Служба остановлена"
            LANG_STRINGS["step_install_binary"]="Установка бинарного файла..."
            LANG_STRINGS["info_binary_installed"]="Бинарный файл установлен: %s (версия: %s)"
            LANG_STRINGS["step_handle_config"]="Обработка файла конфигурации..."
            LANG_STRINGS["warn_config_exists"]="Обнаружен существующий файл конфигурации: %s"
            LANG_STRINGS["config_choice_prompt"]="Пожалуйста, выберите действие:"
            LANG_STRINGS["config_choice_1"]="Сохранить существующую конфигурацию (по умолчанию, рекомендуется)"
            LANG_STRINGS["config_choice_2"]="Заменить новым примером конфигурации (старая конфигурация будет сохранена)"
            LANG_STRINGS["config_choice_3"]="Объединить конфигурацию (сохранить старую, скопировать пример как config.example.yaml)"
            LANG_STRINGS["config_select"]="Выберите [1/2/3] (по умолчанию: 1): "
            LANG_STRINGS["info_config_kept"]="Существующая конфигурация сохранена"
            LANG_STRINGS["info_config_example_copied"]="Пример конфигурации скопирован в: %s"
            LANG_STRINGS["info_config_backed_up"]="Старая конфигурация сохранена в: %s"
            LANG_STRINGS["warn_config_updated"]="Файл конфигурации обновлён, отредактируйте %s для настройки API-ключей и т.д."
            LANG_STRINGS["info_config_merged"]="Существующая конфигурация сохранена, пример скопирован для справки"
            LANG_STRINGS["info_invalid_choice"]="Неверный выбор, существующая конфигурация сохранена"
            LANG_STRINGS["warn_config_created"]="Файл конфигурации создан: %s"
            LANG_STRINGS["warn_config_edit"]="Пожалуйста, отредактируйте файл конфигурации и введите ваши API-ключи и параметры торговли!"
            LANG_STRINGS["step_install_service"]="Установка службы systemd..."
            LANG_STRINGS["info_service_installed"]="Служба systemd установлена и включена"
            LANG_STRINGS["step_set_permissions"]="Установка прав доступа..."
            LANG_STRINGS["info_permissions_set"]="Права доступа установлены"
            LANG_STRINGS["step_copy_scripts"]="Копирование вспомогательных скриптов..."
            LANG_STRINGS["info_scripts_copied"]="Вспомогательные скрипты скопированы"
            LANG_STRINGS["prompt_start_service"]="Запустить службу QuantMesh сейчас? [y/N] (по умолчанию: N): "
            LANG_STRINGS["step_start_service"]="Запуск службы..."
            LANG_STRINGS["info_service_started"]="Служба успешно запущена"
            LANG_STRINGS["error_service_failed"]="Не удалось запустить службу, проверьте журналы: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="Служба не запущена. Вы можете запустить её позже командой:"
            LANG_STRINGS["success_title"]="Установка QuantMesh завершена!"
            LANG_STRINGS["info_install_dir"]="Каталог установки: %s"
            LANG_STRINGS["info_config_file"]="Файл конфигурации: %s"
            LANG_STRINGS["info_data_dir"]="Каталог данных: %s"
            LANG_STRINGS["info_backup_dir"]="Каталог резервных копий: %s"
            LANG_STRINGS["info_logs_dir"]="Каталог журналов: %s"
            LANG_STRINGS["common_commands"]="Часто используемые команды:"
            LANG_STRINGS["cmd_start"]="Запустить службу:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="Остановить службу:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="Перезапустить службу:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="Проверить статус:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="Просмотр журналов:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="Веб-интерфейс:    http://localhost:28888"
            LANG_STRINGS["important_note"]="Важное примечание:"
            LANG_STRINGS["note_edit_config"]="Сначала отредактируйте файл конфигурации и введите API-ключи вашей биржи:"
            LANG_STRINGS["step_start_install"]="Начало установки..."
            ;;
        "hi")
            LANG_STRINGS["title"]="QuantMesh इंस्टॉलर"
            LANG_STRINGS["error_root"]="कृपया इस स्क्रिप्ट को root अधिकारों से चलाएँ: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="पता लगाया गया ऑपरेटिंग सिस्टम: %s %s"
            LANG_STRINGS["error_systemd"]="systemd उपलब्ध नहीं है। यह इंस्टॉलेशन स्क्रिप्ट केवल systemd वाले Linux सिस्टम का समर्थन करती है।"
            LANG_STRINGS["info_systemd"]="systemd उपलब्ध है"
            LANG_STRINGS["error_arch"]="असमर्थित आर्किटेक्चर: %s"
            LANG_STRINGS["error_binary_not_found"]="QuantMesh बाइनरी फ़ाइल नहीं मिली। कृपया सुनिश्चित करें कि आप इस स्क्रिप्ट को निकाले गए डायरेक्टरी में चला रहे हैं।"
            LANG_STRINGS["error_binary_expected"]="अपेक्षित: %s या quantmesh"
            LANG_STRINGS["info_binary_found"]="बाइनरी फ़ाइल मिली: %s"
            LANG_STRINGS["warn_config_not_found"]="config.example.yaml नहीं मिला, कॉन्फ़िगरेशन फ़ाइल इंस्टॉलेशन छोड़ा जाएगा"
            LANG_STRINGS["info_config_found"]="कॉन्फ़िगरेशन उदाहरण मिला: %s"
            LANG_STRINGS["warn_service_not_found"]="quantmesh.service टेम्पलेट नहीं मिला, बिल्ट-इन टेम्पलेट उपयोग किया जाएगा"
            LANG_STRINGS["info_service_found"]="सेवा फ़ाइल टेम्पलेट मिला: %s"
            LANG_STRINGS["step_create_user"]="quantmesh उपयोगकर्ता बनाया जा रहा है..."
            LANG_STRINGS["info_user_created"]="उपयोगकर्ता quantmesh बनाया गया"
            LANG_STRINGS["info_user_exists"]="उपयोगकर्ता quantmesh पहले से मौजूद है"
            LANG_STRINGS["step_create_dirs"]="इंस्टॉलेशन डायरेक्टरी बनाई जा रही है..."
            LANG_STRINGS["info_dirs_created"]="डायरेक्टरी बनाई गई: %s"
            LANG_STRINGS["step_stop_service"]="मौजूदा %s सेवा रोकी जा रही है..."
            LANG_STRINGS["info_service_stopped"]="सेवा रोकी गई"
            LANG_STRINGS["step_install_binary"]="बाइनरी फ़ाइल इंस्टॉल की जा रही है..."
            LANG_STRINGS["info_binary_installed"]="बाइनरी फ़ाइल इंस्टॉल की गई: %s (संस्करण: %s)"
            LANG_STRINGS["step_handle_config"]="कॉन्फ़िगरेशन फ़ाइल संभाली जा रही है..."
            LANG_STRINGS["warn_config_exists"]="मौजूदा कॉन्फ़िगरेशन फ़ाइल पाई गई: %s"
            LANG_STRINGS["config_choice_prompt"]="कृपया चुनें कि कैसे आगे बढ़ना है:"
            LANG_STRINGS["config_choice_1"]="मौजूदा कॉन्फ़िगरेशन रखें (डिफ़ॉल्ट, अनुशंसित)"
            LANG_STRINGS["config_choice_2"]="नए उदाहरण कॉन्फ़िगरेशन से ओवरराइट करें (पुराना बैकअप लिया जाएगा)"
            LANG_STRINGS["config_choice_3"]="कॉन्फ़िगरेशन मर्ज करें (पुराना रखें, उदाहरण को config.example.yaml के रूप में कॉपी करें)"
            LANG_STRINGS["config_select"]="चुनें [1/2/3] (डिफ़ॉल्ट: 1): "
            LANG_STRINGS["info_config_kept"]="मौजूदा कॉन्फ़िगरेशन रखा गया"
            LANG_STRINGS["info_config_example_copied"]="उदाहरण कॉन्फ़िगरेशन कॉपी किया गया: %s"
            LANG_STRINGS["info_config_backed_up"]="पुराना कॉन्फ़िगरेशन बैकअप लिया गया: %s"
            LANG_STRINGS["warn_config_updated"]="कॉन्फ़िगरेशन फ़ाइल अपडेट की गई, कृपया %s संपादित करके API कुंजी आदि सेट करें"
            LANG_STRINGS["info_config_merged"]="मौजूदा कॉन्फ़िगरेशन रखा गया, उदाहरण कॉन्फ़िगरेशन संदर्भ के लिए कॉपी किया गया"
            LANG_STRINGS["info_invalid_choice"]="अमान्य चयन, मौजूदा कॉन्फ़िगरेशन रखा गया"
            LANG_STRINGS["warn_config_created"]="कॉन्फ़िगरेशन फ़ाइल बनाई गई: %s"
            LANG_STRINGS["warn_config_edit"]="कृपया कॉन्फ़िगरेशन फ़ाइल संपादित करें और अपनी API कुंजी और ट्रेडिंग पैरामीटर दर्ज करें!"
            LANG_STRINGS["step_install_service"]="systemd सेवा इंस्टॉल की जा रही है..."
            LANG_STRINGS["info_service_installed"]="systemd सेवा इंस्टॉल और सक्षम की गई"
            LANG_STRINGS["step_set_permissions"]="फ़ाइल अनुमतियाँ सेट की जा रही हैं..."
            LANG_STRINGS["info_permissions_set"]="अनुमतियाँ सेट की गईं"
            LANG_STRINGS["step_copy_scripts"]="सहायक स्क्रिप्ट कॉपी की जा रही हैं..."
            LANG_STRINGS["info_scripts_copied"]="सहायक स्क्रिप्ट कॉपी की गईं"
            LANG_STRINGS["prompt_start_service"]="क्या आप अभी QuantMesh सेवा शुरू करना चाहते हैं? [y/N] (डिफ़ॉल्ट: N): "
            LANG_STRINGS["step_start_service"]="सेवा शुरू की जा रही है..."
            LANG_STRINGS["info_service_started"]="सेवा सफलतापूर्वक शुरू हुई"
            LANG_STRINGS["error_service_failed"]="सेवा शुरू करने में विफल, कृपया लॉग जाँचें: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="सेवा शुरू नहीं की गई। आप बाद में इस कमांड से शुरू कर सकते हैं:"
            LANG_STRINGS["success_title"]="QuantMesh इंस्टॉलेशन पूर्ण!"
            LANG_STRINGS["info_install_dir"]="इंस्टॉलेशन डायरेक्टरी: %s"
            LANG_STRINGS["info_config_file"]="कॉन्फ़िगरेशन फ़ाइल: %s"
            LANG_STRINGS["info_data_dir"]="डेटा डायरेक्टरी: %s"
            LANG_STRINGS["info_backup_dir"]="बैकअप डायरेक्टरी: %s"
            LANG_STRINGS["info_logs_dir"]="लॉग डायरेक्टरी: %s"
            LANG_STRINGS["common_commands"]="सामान्य कमांड:"
            LANG_STRINGS["cmd_start"]="सेवा शुरू करें:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="सेवा रोकें:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="सेवा पुनरारंभ करें:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="स्थिति देखें:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="लॉग देखें:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="वेब इंटरफ़ेस:    http://localhost:28888"
            LANG_STRINGS["important_note"]="महत्वपूर्ण सूचना:"
            LANG_STRINGS["note_edit_config"]="कृपया पहले कॉन्फ़िगरेशन फ़ाइल संपादित करें और अपने एक्सचेंज API कुंजी दर्ज करें:"
            LANG_STRINGS["step_start_install"]="इंस्टॉलेशन शुरू हो रहा है..."
            ;;
        "pt")
            LANG_STRINGS["title"]="Instalador do QuantMesh"
            LANG_STRINGS["error_root"]="Por favor, execute este script com permissões root: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="Sistema operacional detectado: %s %s"
            LANG_STRINGS["error_systemd"]="systemd não está disponível. Este script de instalação suporta apenas sistemas Linux com systemd."
            LANG_STRINGS["info_systemd"]="systemd está disponível"
            LANG_STRINGS["error_arch"]="Arquitetura não suportada: %s"
            LANG_STRINGS["error_binary_not_found"]="Arquivo binário do QuantMesh não encontrado. Certifique-se de executar este script no diretório extraído."
            LANG_STRINGS["error_binary_expected"]="Esperado encontrar: %s ou quantmesh"
            LANG_STRINGS["info_binary_found"]="Arquivo binário encontrado: %s"
            LANG_STRINGS["warn_config_not_found"]="config.example.yaml não encontrado, a instalação do arquivo de configuração será ignorada"
            LANG_STRINGS["info_config_found"]="Exemplo de configuração encontrado: %s"
            LANG_STRINGS["warn_service_not_found"]="Modelo quantmesh.service não encontrado, será usado o modelo integrado"
            LANG_STRINGS["info_service_found"]="Modelo de arquivo de serviço encontrado: %s"
            LANG_STRINGS["step_create_user"]="Criando usuário quantmesh..."
            LANG_STRINGS["info_user_created"]="Usuário quantmesh criado"
            LANG_STRINGS["info_user_exists"]="Usuário quantmesh já existe"
            LANG_STRINGS["step_create_dirs"]="Criando diretórios de instalação..."
            LANG_STRINGS["info_dirs_created"]="Diretórios criados: %s"
            LANG_STRINGS["step_stop_service"]="Parando serviço %s existente..."
            LANG_STRINGS["info_service_stopped"]="Serviço parado"
            LANG_STRINGS["step_install_binary"]="Instalando arquivo binário..."
            LANG_STRINGS["info_binary_installed"]="Arquivo binário instalado: %s (versão: %s)"
            LANG_STRINGS["step_handle_config"]="Processando arquivo de configuração..."
            LANG_STRINGS["warn_config_exists"]="Arquivo de configuração existente detectado: %s"
            LANG_STRINGS["config_choice_prompt"]="Por favor, selecione como proceder:"
            LANG_STRINGS["config_choice_1"]="Manter configuração existente (padrão, recomendado)"
            LANG_STRINGS["config_choice_2"]="Substituir pelo novo exemplo de configuração (a configuração antiga será salva)"
            LANG_STRINGS["config_choice_3"]="Mesclar configuração (manter antiga, copiar exemplo como config.example.yaml)"
            LANG_STRINGS["config_select"]="Selecione [1/2/3] (padrão: 1): "
            LANG_STRINGS["info_config_kept"]="Configuração existente mantida"
            LANG_STRINGS["info_config_example_copied"]="Exemplo de configuração copiado para: %s"
            LANG_STRINGS["info_config_backed_up"]="Configuração antiga salva em: %s"
            LANG_STRINGS["warn_config_updated"]="Arquivo de configuração atualizado, edite %s para configurar suas chaves API, etc."
            LANG_STRINGS["info_config_merged"]="Configuração existente mantida, exemplo copiado para referência"
            LANG_STRINGS["info_invalid_choice"]="Seleção inválida, mantendo configuração existente"
            LANG_STRINGS["warn_config_created"]="Arquivo de configuração criado: %s"
            LANG_STRINGS["warn_config_edit"]="Por favor, edite o arquivo de configuração e insira suas chaves API e parâmetros de negociação!"
            LANG_STRINGS["step_install_service"]="Instalando serviço systemd..."
            LANG_STRINGS["info_service_installed"]="Serviço systemd instalado e habilitado"
            LANG_STRINGS["step_set_permissions"]="Definindo permissões de arquivos..."
            LANG_STRINGS["info_permissions_set"]="Permissões definidas"
            LANG_STRINGS["step_copy_scripts"]="Copiando scripts auxiliares..."
            LANG_STRINGS["info_scripts_copied"]="Scripts auxiliares copiados"
            LANG_STRINGS["prompt_start_service"]="Deseja iniciar o serviço QuantMesh agora? [y/N] (padrão: N): "
            LANG_STRINGS["step_start_service"]="Iniciando serviço..."
            LANG_STRINGS["info_service_started"]="Serviço iniciado com sucesso"
            LANG_STRINGS["error_service_failed"]="Falha ao iniciar o serviço, verifique os logs: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="Serviço não iniciado. Você pode iniciá-lo depois com o seguinte comando:"
            LANG_STRINGS["success_title"]="Instalação do QuantMesh concluída!"
            LANG_STRINGS["info_install_dir"]="Diretório de instalação: %s"
            LANG_STRINGS["info_config_file"]="Arquivo de configuração: %s"
            LANG_STRINGS["info_data_dir"]="Diretório de dados: %s"
            LANG_STRINGS["info_backup_dir"]="Diretório de backup: %s"
            LANG_STRINGS["info_logs_dir"]="Diretório de logs: %s"
            LANG_STRINGS["common_commands"]="Comandos comuns:"
            LANG_STRINGS["cmd_start"]="Iniciar serviço:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="Parar serviço:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="Reiniciar serviço:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="Ver status:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="Ver logs:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="Interface web:    http://localhost:28888"
            LANG_STRINGS["important_note"]="Nota importante:"
            LANG_STRINGS["note_edit_config"]="Edite primeiro o arquivo de configuração e insira as chaves API da sua corretora:"
            LANG_STRINGS["step_start_install"]="Iniciando instalação..."
            ;;
        "de")
            LANG_STRINGS["title"]="QuantMesh Installationsprogramm"
            LANG_STRINGS["error_root"]="Bitte führen Sie dieses Skript als root aus: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="Erkanntes Betriebssystem: %s %s"
            LANG_STRINGS["error_systemd"]="systemd ist nicht verfügbar. Dieses Installationsskript unterstützt nur Linux-Systeme mit systemd."
            LANG_STRINGS["info_systemd"]="systemd ist verfügbar"
            LANG_STRINGS["error_arch"]="Nicht unterstützte Architektur: %s"
            LANG_STRINGS["error_binary_not_found"]="QuantMesh-Binärdatei nicht gefunden. Stellen Sie sicher, dass Sie dieses Skript im entpackten Verzeichnis ausführen."
            LANG_STRINGS["error_binary_expected"]="Erwartet: %s oder quantmesh"
            LANG_STRINGS["info_binary_found"]="Binärdatei gefunden: %s"
            LANG_STRINGS["warn_config_not_found"]="config.example.yaml nicht gefunden, Installation der Konfigurationsdatei wird übersprungen"
            LANG_STRINGS["info_config_found"]="Konfigurationsbeispiel gefunden: %s"
            LANG_STRINGS["warn_service_not_found"]="quantmesh.service-Vorlage nicht gefunden, eingebaute Vorlage wird verwendet"
            LANG_STRINGS["info_service_found"]="Dienstdatei-Vorlage gefunden: %s"
            LANG_STRINGS["step_create_user"]="Erstelle Benutzer quantmesh..."
            LANG_STRINGS["info_user_created"]="Benutzer quantmesh erstellt"
            LANG_STRINGS["info_user_exists"]="Benutzer quantmesh existiert bereits"
            LANG_STRINGS["step_create_dirs"]="Erstelle Installationsverzeichnisse..."
            LANG_STRINGS["info_dirs_created"]="Verzeichnisse erstellt: %s"
            LANG_STRINGS["step_stop_service"]="Stoppe bestehenden Dienst %s..."
            LANG_STRINGS["info_service_stopped"]="Dienst gestoppt"
            LANG_STRINGS["step_install_binary"]="Installiere Binärdatei..."
            LANG_STRINGS["info_binary_installed"]="Binärdatei installiert: %s (Version: %s)"
            LANG_STRINGS["step_handle_config"]="Verarbeite Konfigurationsdatei..."
            LANG_STRINGS["warn_config_exists"]="Vorhandene Konfigurationsdatei erkannt: %s"
            LANG_STRINGS["config_choice_prompt"]="Bitte wählen Sie, wie fortgefahren werden soll:"
            LANG_STRINGS["config_choice_1"]="Bestehende Konfiguration beibehalten (Standard, empfohlen)"
            LANG_STRINGS["config_choice_2"]="Mit neuem Konfigurationsbeispiel überschreiben (alte Konfiguration wird gesichert)"
            LANG_STRINGS["config_choice_3"]="Konfiguration zusammenführen (alte behalten, Beispiel als config.example.yaml kopieren)"
            LANG_STRINGS["config_select"]="Wählen Sie [1/2/3] (Standard: 1): "
            LANG_STRINGS["info_config_kept"]="Bestehende Konfiguration beibehalten"
            LANG_STRINGS["info_config_example_copied"]="Konfigurationsbeispiel kopiert nach: %s"
            LANG_STRINGS["info_config_backed_up"]="Alte Konfiguration gesichert in: %s"
            LANG_STRINGS["warn_config_updated"]="Konfigurationsdatei aktualisiert, bitte bearbeiten Sie %s um Ihre API-Schlüssel usw. einzurichten"
            LANG_STRINGS["info_config_merged"]="Bestehende Konfiguration beibehalten, Beispielkonfiguration als Referenz kopiert"
            LANG_STRINGS["info_invalid_choice"]="Ungültige Auswahl, bestehende Konfiguration wird beibehalten"
            LANG_STRINGS["warn_config_created"]="Konfigurationsdatei erstellt: %s"
            LANG_STRINGS["warn_config_edit"]="Bitte bearbeiten Sie die Konfigurationsdatei und geben Sie Ihre API-Schlüssel und Handelsparameter ein!"
            LANG_STRINGS["step_install_service"]="Installiere systemd-Dienst..."
            LANG_STRINGS["info_service_installed"]="systemd-Dienst installiert und aktiviert"
            LANG_STRINGS["step_set_permissions"]="Setze Dateiberechtigungen..."
            LANG_STRINGS["info_permissions_set"]="Berechtigungen gesetzt"
            LANG_STRINGS["step_copy_scripts"]="Kopiere Hilfsskripte..."
            LANG_STRINGS["info_scripts_copied"]="Hilfsskripte kopiert"
            LANG_STRINGS["prompt_start_service"]="Möchten Sie den QuantMesh-Dienst jetzt starten? [y/N] (Standard: N): "
            LANG_STRINGS["step_start_service"]="Starte Dienst..."
            LANG_STRINGS["info_service_started"]="Dienst erfolgreich gestartet"
            LANG_STRINGS["error_service_failed"]="Dienst konnte nicht gestartet werden, bitte Protokolle prüfen: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="Dienst nicht gestartet. Sie können ihn später mit folgendem Befehl starten:"
            LANG_STRINGS["success_title"]="QuantMesh Installation abgeschlossen!"
            LANG_STRINGS["info_install_dir"]="Installationsverzeichnis: %s"
            LANG_STRINGS["info_config_file"]="Konfigurationsdatei: %s"
            LANG_STRINGS["info_data_dir"]="Datenverzeichnis: %s"
            LANG_STRINGS["info_backup_dir"]="Sicherungsverzeichnis: %s"
            LANG_STRINGS["info_logs_dir"]="Protokollverzeichnis: %s"
            LANG_STRINGS["common_commands"]="Häufige Befehle:"
            LANG_STRINGS["cmd_start"]="Dienst starten:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="Dienst stoppen:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="Dienst neustarten:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="Status prüfen:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="Protokolle anzeigen:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="Weboberfläche:    http://localhost:28888"
            LANG_STRINGS["important_note"]="Wichtiger Hinweis:"
            LANG_STRINGS["note_edit_config"]="Bitte bearbeiten Sie zuerst die Konfigurationsdatei und geben Sie Ihre Exchange-API-Schlüssel ein:"
            LANG_STRINGS["step_start_install"]="Starte Installation..."
            ;;
        "ko")
            LANG_STRINGS["title"]="QuantMesh 설치 프로그램"
            LANG_STRINGS["error_root"]="root 권한으로 이 스크립트를 실행해 주세요: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="감지된 운영체제: %s %s"
            LANG_STRINGS["error_systemd"]="systemd를 사용할 수 없습니다. 이 설치 스크립트는 systemd를 사용하는 Linux 시스템만 지원합니다."
            LANG_STRINGS["info_systemd"]="systemd 사용 가능"
            LANG_STRINGS["error_arch"]="지원되지 않는 아키텍처: %s"
            LANG_STRINGS["error_binary_not_found"]="QuantMesh 바이너리 파일을 찾을 수 없습니다. 압축 해제된 디렉터리에서 이 스크립트를 실행하고 있는지 확인해 주세요."
            LANG_STRINGS["error_binary_expected"]="예상 파일: %s 또는 quantmesh"
            LANG_STRINGS["info_binary_found"]="바이너리 파일 발견: %s"
            LANG_STRINGS["warn_config_not_found"]="config.example.yaml을 찾을 수 없습니다. 설정 파일 설치를 건너뜁니다"
            LANG_STRINGS["info_config_found"]="설정 예제 발견: %s"
            LANG_STRINGS["warn_service_not_found"]="quantmesh.service 템플릿을 찾을 수 없습니다. 내장 템플릿을 사용합니다"
            LANG_STRINGS["info_service_found"]="서비스 파일 템플릿 발견: %s"
            LANG_STRINGS["step_create_user"]="quantmesh 사용자 생성 중..."
            LANG_STRINGS["info_user_created"]="사용자 quantmesh 생성됨"
            LANG_STRINGS["info_user_exists"]="사용자 quantmesh이(가) 이미 존재합니다"
            LANG_STRINGS["step_create_dirs"]="설치 디렉터리 생성 중..."
            LANG_STRINGS["info_dirs_created"]="디렉터리 생성됨: %s"
            LANG_STRINGS["step_stop_service"]="기존 %s 서비스 중지 중..."
            LANG_STRINGS["info_service_stopped"]="서비스 중지됨"
            LANG_STRINGS["step_install_binary"]="바이너리 파일 설치 중..."
            LANG_STRINGS["info_binary_installed"]="바이너리 파일 설치됨: %s (버전: %s)"
            LANG_STRINGS["step_handle_config"]="설정 파일 처리 중..."
            LANG_STRINGS["warn_config_exists"]="기존 설정 파일 감지됨: %s"
            LANG_STRINGS["config_choice_prompt"]="진행 방법을 선택해 주세요:"
            LANG_STRINGS["config_choice_1"]="기존 설정 유지 (기본값, 권장)"
            LANG_STRINGS["config_choice_2"]="새 예제 설정으로 덮어쓰기 (기존 설정 백업됨)"
            LANG_STRINGS["config_choice_3"]="설정 병합 (기존 유지, 예제를 config.example.yaml로 복사)"
            LANG_STRINGS["config_select"]="선택 [1/2/3] (기본값: 1): "
            LANG_STRINGS["info_config_kept"]="기존 설정 유지됨"
            LANG_STRINGS["info_config_example_copied"]="예제 설정 복사됨: %s"
            LANG_STRINGS["info_config_backed_up"]="기존 설정 백업됨: %s"
            LANG_STRINGS["warn_config_updated"]="설정 파일이 업데이트되었습니다. %s를 편집하여 API 키 등을 설정해 주세요"
            LANG_STRINGS["info_config_merged"]="기존 설정 유지됨, 예제 설정이 참조용으로 복사됨"
            LANG_STRINGS["info_invalid_choice"]="잘못된 선택, 기존 설정 유지"
            LANG_STRINGS["warn_config_created"]="설정 파일 생성됨: %s"
            LANG_STRINGS["warn_config_edit"]="설정 파일을 편집하여 API 키와 거래 매개변수를 입력해 주세요!"
            LANG_STRINGS["step_install_service"]="systemd 서비스 설치 중..."
            LANG_STRINGS["info_service_installed"]="systemd 서비스 설치 및 활성화됨"
            LANG_STRINGS["step_set_permissions"]="파일 권한 설정 중..."
            LANG_STRINGS["info_permissions_set"]="권한 설정됨"
            LANG_STRINGS["step_copy_scripts"]="보조 스크립트 복사 중..."
            LANG_STRINGS["info_scripts_copied"]="보조 스크립트 복사됨"
            LANG_STRINGS["prompt_start_service"]="지금 QuantMesh 서비스를 시작하시겠습니까? [y/N] (기본값: N): "
            LANG_STRINGS["step_start_service"]="서비스 시작 중..."
            LANG_STRINGS["info_service_started"]="서비스가 성공적으로 시작됨"
            LANG_STRINGS["error_service_failed"]="서비스 시작 실패, 로그를 확인해 주세요: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="서비스가 시작되지 않았습니다. 나중에 다음 명령으로 시작할 수 있습니다:"
            LANG_STRINGS["success_title"]="QuantMesh 설치 완료!"
            LANG_STRINGS["info_install_dir"]="설치 디렉터리: %s"
            LANG_STRINGS["info_config_file"]="설정 파일: %s"
            LANG_STRINGS["info_data_dir"]="데이터 디렉터리: %s"
            LANG_STRINGS["info_backup_dir"]="백업 디렉터리: %s"
            LANG_STRINGS["info_logs_dir"]="로그 디렉터리: %s"
            LANG_STRINGS["common_commands"]="자주 사용하는 명령어:"
            LANG_STRINGS["cmd_start"]="서비스 시작:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="서비스 중지:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="서비스 재시작:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="상태 확인:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="로그 확인:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="웹 인터페이스:    http://localhost:28888"
            LANG_STRINGS["important_note"]="중요 안내:"
            LANG_STRINGS["note_edit_config"]="먼저 설정 파일을 편집하여 거래소 API 키를 입력해 주세요:"
            LANG_STRINGS["step_start_install"]="설치 시작 중..."
            ;;
        "ar")
            LANG_STRINGS["title"]="مثبت QuantMesh"
            LANG_STRINGS["error_root"]="يرجى تشغيل هذا البرنامج النصي بصلاحيات root: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="نظام التشغيل المكتشف: %s %s"
            LANG_STRINGS["error_systemd"]="systemd غير متوفر. يدعم هذا البرنامج النصي فقط أنظمة Linux التي تستخدم systemd."
            LANG_STRINGS["info_systemd"]="systemd متوفر"
            LANG_STRINGS["error_arch"]="بنية غير مدعومة: %s"
            LANG_STRINGS["error_binary_not_found"]="لم يتم العثور على ملف QuantMesh الثنائي. تأكد من تشغيل هذا البرنامج النصي في المجلد المستخرج."
            LANG_STRINGS["error_binary_expected"]="المتوقع: %s أو quantmesh"
            LANG_STRINGS["info_binary_found"]="تم العثور على الملف الثنائي: %s"
            LANG_STRINGS["warn_config_not_found"]="لم يتم العثور على config.example.yaml، سيتم تخطي تثبيت ملف الإعدادات"
            LANG_STRINGS["info_config_found"]="تم العثور على مثال الإعدادات: %s"
            LANG_STRINGS["warn_service_not_found"]="لم يتم العثور على قالب quantmesh.service، سيتم استخدام القالب المدمج"
            LANG_STRINGS["info_service_found"]="تم العثور على قالب ملف الخدمة: %s"
            LANG_STRINGS["step_create_user"]="جاري إنشاء مستخدم quantmesh..."
            LANG_STRINGS["info_user_created"]="تم إنشاء المستخدم quantmesh"
            LANG_STRINGS["info_user_exists"]="المستخدم quantmesh موجود بالفعل"
            LANG_STRINGS["step_create_dirs"]="جاري إنشاء مجلدات التثبيت..."
            LANG_STRINGS["info_dirs_created"]="تم إنشاء المجلدات: %s"
            LANG_STRINGS["step_stop_service"]="جاري إيقاف الخدمة الحالية %s..."
            LANG_STRINGS["info_service_stopped"]="تم إيقاف الخدمة"
            LANG_STRINGS["step_install_binary"]="جاري تثبيت الملف الثنائي..."
            LANG_STRINGS["info_binary_installed"]="تم تثبيت الملف الثنائي: %s (الإصدار: %s)"
            LANG_STRINGS["step_handle_config"]="جاري معالجة ملف الإعدادات..."
            LANG_STRINGS["warn_config_exists"]="تم اكتشاف ملف إعدادات موجود: %s"
            LANG_STRINGS["config_choice_prompt"]="يرجى اختيار كيفية المتابعة:"
            LANG_STRINGS["config_choice_1"]="الاحتفاظ بالإعدادات الحالية (افتراضي، مُوصى)"
            LANG_STRINGS["config_choice_2"]="الاستبدال بمثال الإعدادات الجديد (سيتم حفظ نسخة احتياطية من الإعدادات القديمة)"
            LANG_STRINGS["config_choice_3"]="دمج الإعدادات (الاحتفاظ بالقديمة، نسخ المثال كـ config.example.yaml)"
            LANG_STRINGS["config_select"]="اختر [1/2/3] (افتراضي: 1): "
            LANG_STRINGS["info_config_kept"]="تم الاحتفاظ بالإعدادات الحالية"
            LANG_STRINGS["info_config_example_copied"]="تم نسخ مثال الإعدادات إلى: %s"
            LANG_STRINGS["info_config_backed_up"]="تم حفظ نسخة احتياطية من الإعدادات القديمة في: %s"
            LANG_STRINGS["warn_config_updated"]="تم تحديث ملف الإعدادات، يرجى تحرير %s لإعداد مفاتيح API الخاصة بك وغيرها"
            LANG_STRINGS["info_config_merged"]="تم الاحتفاظ بالإعدادات الحالية، تم نسخ مثال الإعدادات للمرجع"
            LANG_STRINGS["info_invalid_choice"]="اختيار غير صالح، الاحتفاظ بالإعدادات الحالية"
            LANG_STRINGS["warn_config_created"]="تم إنشاء ملف الإعدادات: %s"
            LANG_STRINGS["warn_config_edit"]="يرجى تحرير ملف الإعدادات وإدخال مفاتيح API ومعاملات التداول الخاصة بك!"
            LANG_STRINGS["step_install_service"]="جاري تثبيت خدمة systemd..."
            LANG_STRINGS["info_service_installed"]="تم تثبيت وتفعيل خدمة systemd"
            LANG_STRINGS["step_set_permissions"]="جاري تعيين أذونات الملفات..."
            LANG_STRINGS["info_permissions_set"]="تم تعيين الأذونات"
            LANG_STRINGS["step_copy_scripts"]="جاري نسخ البرامج النصية المساعدة..."
            LANG_STRINGS["info_scripts_copied"]="تم نسخ البرامج النصية المساعدة"
            LANG_STRINGS["prompt_start_service"]="هل تريد بدء خدمة QuantMesh الآن؟ [y/N] (افتراضي: N): "
            LANG_STRINGS["step_start_service"]="جاري بدء الخدمة..."
            LANG_STRINGS["info_service_started"]="تم بدء الخدمة بنجاح"
            LANG_STRINGS["error_service_failed"]="فشل بدء الخدمة، يرجى التحقق من السجلات: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="لم يتم بدء الخدمة. يمكنك بدؤها لاحقاً بالأمر التالي:"
            LANG_STRINGS["success_title"]="اكتمل تثبيت QuantMesh!"
            LANG_STRINGS["info_install_dir"]="مجلد التثبيت: %s"
            LANG_STRINGS["info_config_file"]="ملف الإعدادات: %s"
            LANG_STRINGS["info_data_dir"]="مجلد البيانات: %s"
            LANG_STRINGS["info_backup_dir"]="مجلد النسخ الاحتياطية: %s"
            LANG_STRINGS["info_logs_dir"]="مجلد السجلات: %s"
            LANG_STRINGS["common_commands"]="الأوامر الشائعة:"
            LANG_STRINGS["cmd_start"]="بدء الخدمة:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="إيقاف الخدمة:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="إعادة تشغيل الخدمة:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="عرض الحالة:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="عرض السجلات:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="واجهة الويب:    http://localhost:28888"
            LANG_STRINGS["important_note"]="ملاحظة مهمة:"
            LANG_STRINGS["note_edit_config"]="يرجى تحرير ملف الإعدادات أولاً وإدخال مفاتيح API الخاصة بمنصة التداول:"
            LANG_STRINGS["step_start_install"]="بدء التثبيت..."
            ;;
        "tr")
            LANG_STRINGS["title"]="QuantMesh Yükleyici"
            LANG_STRINGS["error_root"]="Lütfen bu betiği root yetkileriyle çalıştırın: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="Algılanan işletim sistemi: %s %s"
            LANG_STRINGS["error_systemd"]="systemd mevcut değil. Bu yükleme betiği yalnızca systemd kullanan Linux sistemlerini destekler."
            LANG_STRINGS["info_systemd"]="systemd mevcut"
            LANG_STRINGS["error_arch"]="Desteklenmeyen mimari: %s"
            LANG_STRINGS["error_binary_not_found"]="QuantMesh ikili dosyası bulunamadı. Bu betiği çıkartılan dizinde çalıştırdığınızdan emin olun."
            LANG_STRINGS["error_binary_expected"]="Beklenen: %s veya quantmesh"
            LANG_STRINGS["info_binary_found"]="İkili dosya bulundu: %s"
            LANG_STRINGS["warn_config_not_found"]="config.example.yaml bulunamadı, yapılandırma dosyası yüklemesi atlanacak"
            LANG_STRINGS["info_config_found"]="Yapılandırma örneği bulundu: %s"
            LANG_STRINGS["warn_service_not_found"]="quantmesh.service şablonu bulunamadı, yerleşik şablon kullanılacak"
            LANG_STRINGS["info_service_found"]="Hizmet dosyası şablonu bulundu: %s"
            LANG_STRINGS["step_create_user"]="quantmesh kullanıcısı oluşturuluyor..."
            LANG_STRINGS["info_user_created"]="quantmesh kullanıcısı oluşturuldu"
            LANG_STRINGS["info_user_exists"]="quantmesh kullanıcısı zaten mevcut"
            LANG_STRINGS["step_create_dirs"]="Yükleme dizinleri oluşturuluyor..."
            LANG_STRINGS["info_dirs_created"]="Dizinler oluşturuldu: %s"
            LANG_STRINGS["step_stop_service"]="Mevcut %s hizmeti durduruluyor..."
            LANG_STRINGS["info_service_stopped"]="Hizmet durduruldu"
            LANG_STRINGS["step_install_binary"]="İkili dosya yükleniyor..."
            LANG_STRINGS["info_binary_installed"]="İkili dosya yüklendi: %s (sürüm: %s)"
            LANG_STRINGS["step_handle_config"]="Yapılandırma dosyası işleniyor..."
            LANG_STRINGS["warn_config_exists"]="Mevcut yapılandırma dosyası algılandı: %s"
            LANG_STRINGS["config_choice_prompt"]="Lütfen nasıl devam edileceğini seçin:"
            LANG_STRINGS["config_choice_1"]="Mevcut yapılandırmayı koru (varsayılan, önerilen)"
            LANG_STRINGS["config_choice_2"]="Yeni örnek yapılandırmayla değiştir (eski yapılandırma yedeklenecek)"
            LANG_STRINGS["config_choice_3"]="Yapılandırmayı birleştir (eskiyi koru, örneği config.example.yaml olarak kopyala)"
            LANG_STRINGS["config_select"]="Seçin [1/2/3] (varsayılan: 1): "
            LANG_STRINGS["info_config_kept"]="Mevcut yapılandırma korundu"
            LANG_STRINGS["info_config_example_copied"]="Örnek yapılandırma kopyalandı: %s"
            LANG_STRINGS["info_config_backed_up"]="Eski yapılandırma yedeklendi: %s"
            LANG_STRINGS["warn_config_updated"]="Yapılandırma dosyası güncellendi, API anahtarlarınızı ayarlamak için %s dosyasını düzenleyin"
            LANG_STRINGS["info_config_merged"]="Mevcut yapılandırma korundu, örnek yapılandırma referans için kopyalandı"
            LANG_STRINGS["info_invalid_choice"]="Geçersiz seçim, mevcut yapılandırma korunuyor"
            LANG_STRINGS["warn_config_created"]="Yapılandırma dosyası oluşturuldu: %s"
            LANG_STRINGS["warn_config_edit"]="Lütfen yapılandırma dosyasını düzenleyin ve API anahtarlarınızı ve işlem parametrelerinizi girin!"
            LANG_STRINGS["step_install_service"]="systemd hizmeti yükleniyor..."
            LANG_STRINGS["info_service_installed"]="systemd hizmeti yüklendi ve etkinleştirildi"
            LANG_STRINGS["step_set_permissions"]="Dosya izinleri ayarlanıyor..."
            LANG_STRINGS["info_permissions_set"]="İzinler ayarlandı"
            LANG_STRINGS["step_copy_scripts"]="Yardımcı betikler kopyalanıyor..."
            LANG_STRINGS["info_scripts_copied"]="Yardımcı betikler kopyalandı"
            LANG_STRINGS["prompt_start_service"]="QuantMesh hizmetini şimdi başlatmak istiyor musunuz? [y/N] (varsayılan: N): "
            LANG_STRINGS["step_start_service"]="Hizmet başlatılıyor..."
            LANG_STRINGS["info_service_started"]="Hizmet başarıyla başlatıldı"
            LANG_STRINGS["error_service_failed"]="Hizmet başlatılamadı, lütfen günlükleri kontrol edin: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="Hizmet başlatılmadı. Daha sonra şu komutla başlatabilirsiniz:"
            LANG_STRINGS["success_title"]="QuantMesh yüklemesi tamamlandı!"
            LANG_STRINGS["info_install_dir"]="Yükleme dizini: %s"
            LANG_STRINGS["info_config_file"]="Yapılandırma dosyası: %s"
            LANG_STRINGS["info_data_dir"]="Veri dizini: %s"
            LANG_STRINGS["info_backup_dir"]="Yedek dizini: %s"
            LANG_STRINGS["info_logs_dir"]="Günlük dizini: %s"
            LANG_STRINGS["common_commands"]="Sık kullanılan komutlar:"
            LANG_STRINGS["cmd_start"]="Hizmeti başlat:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="Hizmeti durdur:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="Hizmeti yeniden başlat:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="Durumu görüntüle:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="Günlükleri görüntüle:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="Web arayüzü:    http://localhost:28888"
            LANG_STRINGS["important_note"]="Önemli not:"
            LANG_STRINGS["note_edit_config"]="Lütfen önce yapılandırma dosyasını düzenleyin ve borsa API anahtarlarınızı girin:"
            LANG_STRINGS["step_start_install"]="Yükleme başlatılıyor..."
            ;;
        "vi")
            LANG_STRINGS["title"]="Trình cài đặt QuantMesh"
            LANG_STRINGS["error_root"]="Vui lòng chạy tập lệnh này với quyền root: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="Hệ điều hành được phát hiện: %s %s"
            LANG_STRINGS["error_systemd"]="systemd không khả dụng. Tập lệnh cài đặt này chỉ hỗ trợ hệ thống Linux sử dụng systemd."
            LANG_STRINGS["info_systemd"]="systemd khả dụng"
            LANG_STRINGS["error_arch"]="Kiến trúc không được hỗ trợ: %s"
            LANG_STRINGS["error_binary_not_found"]="Không tìm thấy tệp nhị phân QuantMesh. Hãy đảm bảo bạn chạy tập lệnh này trong thư mục đã giải nén."
            LANG_STRINGS["error_binary_expected"]="Mong đợi tìm thấy: %s hoặc quantmesh"
            LANG_STRINGS["info_binary_found"]="Đã tìm thấy tệp nhị phân: %s"
            LANG_STRINGS["warn_config_not_found"]="Không tìm thấy config.example.yaml, sẽ bỏ qua cài đặt tệp cấu hình"
            LANG_STRINGS["info_config_found"]="Đã tìm thấy ví dụ cấu hình: %s"
            LANG_STRINGS["warn_service_not_found"]="Không tìm thấy mẫu quantmesh.service, sẽ sử dụng mẫu tích hợp"
            LANG_STRINGS["info_service_found"]="Đã tìm thấy mẫu tệp dịch vụ: %s"
            LANG_STRINGS["step_create_user"]="Đang tạo người dùng quantmesh..."
            LANG_STRINGS["info_user_created"]="Đã tạo người dùng quantmesh"
            LANG_STRINGS["info_user_exists"]="Người dùng quantmesh đã tồn tại"
            LANG_STRINGS["step_create_dirs"]="Đang tạo thư mục cài đặt..."
            LANG_STRINGS["info_dirs_created"]="Đã tạo thư mục: %s"
            LANG_STRINGS["step_stop_service"]="Đang dừng dịch vụ %s hiện có..."
            LANG_STRINGS["info_service_stopped"]="Đã dừng dịch vụ"
            LANG_STRINGS["step_install_binary"]="Đang cài đặt tệp nhị phân..."
            LANG_STRINGS["info_binary_installed"]="Đã cài đặt tệp nhị phân: %s (phiên bản: %s)"
            LANG_STRINGS["step_handle_config"]="Đang xử lý tệp cấu hình..."
            LANG_STRINGS["warn_config_exists"]="Phát hiện tệp cấu hình hiện có: %s"
            LANG_STRINGS["config_choice_prompt"]="Vui lòng chọn cách tiếp tục:"
            LANG_STRINGS["config_choice_1"]="Giữ cấu hình hiện có (mặc định, khuyến nghị)"
            LANG_STRINGS["config_choice_2"]="Ghi đè bằng cấu hình ví dụ mới (cấu hình cũ sẽ được sao lưu)"
            LANG_STRINGS["config_choice_3"]="Hợp nhất cấu hình (giữ cũ, sao chép ví dụ thành config.example.yaml)"
            LANG_STRINGS["config_select"]="Chọn [1/2/3] (mặc định: 1): "
            LANG_STRINGS["info_config_kept"]="Đã giữ cấu hình hiện có"
            LANG_STRINGS["info_config_example_copied"]="Đã sao chép cấu hình ví dụ đến: %s"
            LANG_STRINGS["info_config_backed_up"]="Đã sao lưu cấu hình cũ tại: %s"
            LANG_STRINGS["warn_config_updated"]="Tệp cấu hình đã được cập nhật, vui lòng chỉnh sửa %s để thiết lập khóa API, v.v."
            LANG_STRINGS["info_config_merged"]="Đã giữ cấu hình hiện có, cấu hình ví dụ đã được sao chép để tham khảo"
            LANG_STRINGS["info_invalid_choice"]="Lựa chọn không hợp lệ, giữ cấu hình hiện có"
            LANG_STRINGS["warn_config_created"]="Đã tạo tệp cấu hình: %s"
            LANG_STRINGS["warn_config_edit"]="Vui lòng chỉnh sửa tệp cấu hình và nhập khóa API cùng các tham số giao dịch của bạn!"
            LANG_STRINGS["step_install_service"]="Đang cài đặt dịch vụ systemd..."
            LANG_STRINGS["info_service_installed"]="Dịch vụ systemd đã được cài đặt và kích hoạt"
            LANG_STRINGS["step_set_permissions"]="Đang thiết lập quyền truy cập tệp..."
            LANG_STRINGS["info_permissions_set"]="Đã thiết lập quyền truy cập"
            LANG_STRINGS["step_copy_scripts"]="Đang sao chép các tập lệnh phụ trợ..."
            LANG_STRINGS["info_scripts_copied"]="Đã sao chép các tập lệnh phụ trợ"
            LANG_STRINGS["prompt_start_service"]="Bạn có muốn khởi động dịch vụ QuantMesh ngay bây giờ không? [y/N] (mặc định: N): "
            LANG_STRINGS["step_start_service"]="Đang khởi động dịch vụ..."
            LANG_STRINGS["info_service_started"]="Dịch vụ đã khởi động thành công"
            LANG_STRINGS["error_service_failed"]="Khởi động dịch vụ thất bại, vui lòng kiểm tra nhật ký: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="Dịch vụ chưa được khởi động. Bạn có thể khởi động sau bằng lệnh:"
            LANG_STRINGS["success_title"]="Cài đặt QuantMesh hoàn tất!"
            LANG_STRINGS["info_install_dir"]="Thư mục cài đặt: %s"
            LANG_STRINGS["info_config_file"]="Tệp cấu hình: %s"
            LANG_STRINGS["info_data_dir"]="Thư mục dữ liệu: %s"
            LANG_STRINGS["info_backup_dir"]="Thư mục sao lưu: %s"
            LANG_STRINGS["info_logs_dir"]="Thư mục nhật ký: %s"
            LANG_STRINGS["common_commands"]="Các lệnh thường dùng:"
            LANG_STRINGS["cmd_start"]="Khởi động dịch vụ:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="Dừng dịch vụ:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="Khởi động lại dịch vụ:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="Xem trạng thái:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="Xem nhật ký:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="Giao diện web:    http://localhost:28888"
            LANG_STRINGS["important_note"]="Lưu ý quan trọng:"
            LANG_STRINGS["note_edit_config"]="Vui lòng chỉnh sửa tệp cấu hình trước và nhập khóa API của sàn giao dịch:"
            LANG_STRINGS["step_start_install"]="Bắt đầu cài đặt..."
            ;;
        "it")
            LANG_STRINGS["title"]="Programma di installazione QuantMesh"
            LANG_STRINGS["error_root"]="Eseguire questo script con i permessi root: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="Sistema operativo rilevato: %s %s"
            LANG_STRINGS["error_systemd"]="systemd non è disponibile. Questo script di installazione supporta solo sistemi Linux con systemd."
            LANG_STRINGS["info_systemd"]="systemd è disponibile"
            LANG_STRINGS["error_arch"]="Architettura non supportata: %s"
            LANG_STRINGS["error_binary_not_found"]="File binario QuantMesh non trovato. Assicurarsi di eseguire questo script nella directory estratta."
            LANG_STRINGS["error_binary_expected"]="Previsto: %s o quantmesh"
            LANG_STRINGS["info_binary_found"]="File binario trovato: %s"
            LANG_STRINGS["warn_config_not_found"]="config.example.yaml non trovato, l'installazione del file di configurazione verrà saltata"
            LANG_STRINGS["info_config_found"]="Esempio di configurazione trovato: %s"
            LANG_STRINGS["warn_service_not_found"]="Modello quantmesh.service non trovato, verrà utilizzato il modello integrato"
            LANG_STRINGS["info_service_found"]="Modello del file di servizio trovato: %s"
            LANG_STRINGS["step_create_user"]="Creazione utente quantmesh..."
            LANG_STRINGS["info_user_created"]="Utente quantmesh creato"
            LANG_STRINGS["info_user_exists"]="L'utente quantmesh esiste già"
            LANG_STRINGS["step_create_dirs"]="Creazione directory di installazione..."
            LANG_STRINGS["info_dirs_created"]="Directory create: %s"
            LANG_STRINGS["step_stop_service"]="Arresto del servizio %s esistente..."
            LANG_STRINGS["info_service_stopped"]="Servizio arrestato"
            LANG_STRINGS["step_install_binary"]="Installazione file binario..."
            LANG_STRINGS["info_binary_installed"]="File binario installato: %s (versione: %s)"
            LANG_STRINGS["step_handle_config"]="Elaborazione file di configurazione..."
            LANG_STRINGS["warn_config_exists"]="File di configurazione esistente rilevato: %s"
            LANG_STRINGS["config_choice_prompt"]="Selezionare come procedere:"
            LANG_STRINGS["config_choice_1"]="Mantenere la configurazione esistente (predefinito, consigliato)"
            LANG_STRINGS["config_choice_2"]="Sovrascrivere con il nuovo esempio di configurazione (la configurazione precedente verrà salvata)"
            LANG_STRINGS["config_choice_3"]="Unire la configurazione (mantenere la vecchia, copiare l'esempio come config.example.yaml)"
            LANG_STRINGS["config_select"]="Selezionare [1/2/3] (predefinito: 1): "
            LANG_STRINGS["info_config_kept"]="Configurazione esistente mantenuta"
            LANG_STRINGS["info_config_example_copied"]="Esempio di configurazione copiato in: %s"
            LANG_STRINGS["info_config_backed_up"]="Configurazione precedente salvata in: %s"
            LANG_STRINGS["warn_config_updated"]="File di configurazione aggiornato, modificare %s per configurare le chiavi API, ecc."
            LANG_STRINGS["info_config_merged"]="Configurazione esistente mantenuta, esempio copiato come riferimento"
            LANG_STRINGS["info_invalid_choice"]="Scelta non valida, configurazione esistente mantenuta"
            LANG_STRINGS["warn_config_created"]="File di configurazione creato: %s"
            LANG_STRINGS["warn_config_edit"]="Modificare il file di configurazione e inserire le chiavi API e i parametri di trading!"
            LANG_STRINGS["step_install_service"]="Installazione servizio systemd..."
            LANG_STRINGS["info_service_installed"]="Servizio systemd installato e abilitato"
            LANG_STRINGS["step_set_permissions"]="Impostazione permessi file..."
            LANG_STRINGS["info_permissions_set"]="Permessi impostati"
            LANG_STRINGS["step_copy_scripts"]="Copia script ausiliari..."
            LANG_STRINGS["info_scripts_copied"]="Script ausiliari copiati"
            LANG_STRINGS["prompt_start_service"]="Avviare il servizio QuantMesh ora? [y/N] (predefinito: N): "
            LANG_STRINGS["step_start_service"]="Avvio servizio..."
            LANG_STRINGS["info_service_started"]="Servizio avviato con successo"
            LANG_STRINGS["error_service_failed"]="Avvio servizio fallito, controllare i log: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="Servizio non avviato. È possibile avviarlo in seguito con il seguente comando:"
            LANG_STRINGS["success_title"]="Installazione di QuantMesh completata!"
            LANG_STRINGS["info_install_dir"]="Directory di installazione: %s"
            LANG_STRINGS["info_config_file"]="File di configurazione: %s"
            LANG_STRINGS["info_data_dir"]="Directory dati: %s"
            LANG_STRINGS["info_backup_dir"]="Directory backup: %s"
            LANG_STRINGS["info_logs_dir"]="Directory log: %s"
            LANG_STRINGS["common_commands"]="Comandi comuni:"
            LANG_STRINGS["cmd_start"]="Avviare servizio:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="Arrestare servizio:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="Riavviare servizio:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="Verificare stato:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="Visualizzare log:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="Interfaccia web:    http://localhost:28888"
            LANG_STRINGS["important_note"]="Nota importante:"
            LANG_STRINGS["note_edit_config"]="Modificare prima il file di configurazione e inserire le chiavi API dell'exchange:"
            LANG_STRINGS["step_start_install"]="Avvio installazione..."
            ;;
        "id")
            LANG_STRINGS["title"]="Penginstal QuantMesh"
            LANG_STRINGS["error_root"]="Silakan jalankan skrip ini dengan hak akses root: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="Sistem operasi terdeteksi: %s %s"
            LANG_STRINGS["error_systemd"]="systemd tidak tersedia. Skrip instalasi ini hanya mendukung sistem Linux yang menggunakan systemd."
            LANG_STRINGS["info_systemd"]="systemd tersedia"
            LANG_STRINGS["error_arch"]="Arsitektur tidak didukung: %s"
            LANG_STRINGS["error_binary_not_found"]="File biner QuantMesh tidak ditemukan. Pastikan Anda menjalankan skrip ini di direktori yang diekstrak."
            LANG_STRINGS["error_binary_expected"]="Diharapkan menemukan: %s atau quantmesh"
            LANG_STRINGS["info_binary_found"]="File biner ditemukan: %s"
            LANG_STRINGS["warn_config_not_found"]="config.example.yaml tidak ditemukan, instalasi file konfigurasi akan dilewati"
            LANG_STRINGS["info_config_found"]="Contoh konfigurasi ditemukan: %s"
            LANG_STRINGS["warn_service_not_found"]="Template quantmesh.service tidak ditemukan, akan menggunakan template bawaan"
            LANG_STRINGS["info_service_found"]="Template file layanan ditemukan: %s"
            LANG_STRINGS["step_create_user"]="Membuat pengguna quantmesh..."
            LANG_STRINGS["info_user_created"]="Pengguna quantmesh dibuat"
            LANG_STRINGS["info_user_exists"]="Pengguna quantmesh sudah ada"
            LANG_STRINGS["step_create_dirs"]="Membuat direktori instalasi..."
            LANG_STRINGS["info_dirs_created"]="Direktori dibuat: %s"
            LANG_STRINGS["step_stop_service"]="Menghentikan layanan %s yang ada..."
            LANG_STRINGS["info_service_stopped"]="Layanan dihentikan"
            LANG_STRINGS["step_install_binary"]="Menginstal file biner..."
            LANG_STRINGS["info_binary_installed"]="File biner diinstal: %s (versi: %s)"
            LANG_STRINGS["step_handle_config"]="Memproses file konfigurasi..."
            LANG_STRINGS["warn_config_exists"]="File konfigurasi yang ada terdeteksi: %s"
            LANG_STRINGS["config_choice_prompt"]="Silakan pilih cara melanjutkan:"
            LANG_STRINGS["config_choice_1"]="Pertahankan konfigurasi yang ada (default, disarankan)"
            LANG_STRINGS["config_choice_2"]="Timpa dengan contoh konfigurasi baru (konfigurasi lama akan dicadangkan)"
            LANG_STRINGS["config_choice_3"]="Gabungkan konfigurasi (pertahankan yang lama, salin contoh sebagai config.example.yaml)"
            LANG_STRINGS["config_select"]="Pilih [1/2/3] (default: 1): "
            LANG_STRINGS["info_config_kept"]="Konfigurasi yang ada dipertahankan"
            LANG_STRINGS["info_config_example_copied"]="Contoh konfigurasi disalin ke: %s"
            LANG_STRINGS["info_config_backed_up"]="Konfigurasi lama dicadangkan di: %s"
            LANG_STRINGS["warn_config_updated"]="File konfigurasi diperbarui, silakan edit %s untuk mengatur kunci API Anda, dll."
            LANG_STRINGS["info_config_merged"]="Konfigurasi yang ada dipertahankan, contoh konfigurasi disalin untuk referensi"
            LANG_STRINGS["info_invalid_choice"]="Pilihan tidak valid, mempertahankan konfigurasi yang ada"
            LANG_STRINGS["warn_config_created"]="File konfigurasi dibuat: %s"
            LANG_STRINGS["warn_config_edit"]="Silakan edit file konfigurasi dan masukkan kunci API serta parameter perdagangan Anda!"
            LANG_STRINGS["step_install_service"]="Menginstal layanan systemd..."
            LANG_STRINGS["info_service_installed"]="Layanan systemd diinstal dan diaktifkan"
            LANG_STRINGS["step_set_permissions"]="Mengatur izin file..."
            LANG_STRINGS["info_permissions_set"]="Izin diatur"
            LANG_STRINGS["step_copy_scripts"]="Menyalin skrip pembantu..."
            LANG_STRINGS["info_scripts_copied"]="Skrip pembantu disalin"
            LANG_STRINGS["prompt_start_service"]="Apakah Anda ingin memulai layanan QuantMesh sekarang? [y/N] (default: N): "
            LANG_STRINGS["step_start_service"]="Memulai layanan..."
            LANG_STRINGS["info_service_started"]="Layanan berhasil dimulai"
            LANG_STRINGS["error_service_failed"]="Gagal memulai layanan, silakan periksa log: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="Layanan tidak dimulai. Anda dapat memulainya nanti dengan perintah berikut:"
            LANG_STRINGS["success_title"]="Instalasi QuantMesh selesai!"
            LANG_STRINGS["info_install_dir"]="Direktori instalasi: %s"
            LANG_STRINGS["info_config_file"]="File konfigurasi: %s"
            LANG_STRINGS["info_data_dir"]="Direktori data: %s"
            LANG_STRINGS["info_backup_dir"]="Direktori cadangan: %s"
            LANG_STRINGS["info_logs_dir"]="Direktori log: %s"
            LANG_STRINGS["common_commands"]="Perintah umum:"
            LANG_STRINGS["cmd_start"]="Mulai layanan:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="Hentikan layanan:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="Mulai ulang layanan:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="Lihat status:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="Lihat log:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="Antarmuka web:    http://localhost:28888"
            LANG_STRINGS["important_note"]="Catatan penting:"
            LANG_STRINGS["note_edit_config"]="Silakan edit file konfigurasi terlebih dahulu dan masukkan kunci API bursa Anda:"
            LANG_STRINGS["step_start_install"]="Memulai instalasi..."
            ;;
        "nl")
            LANG_STRINGS["title"]="QuantMesh Installatieprogramma"
            LANG_STRINGS["error_root"]="Voer dit script uit met root-rechten: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="Gedetecteerd besturingssysteem: %s %s"
            LANG_STRINGS["error_systemd"]="systemd is niet beschikbaar. Dit installatiescript ondersteunt alleen Linux-systemen met systemd."
            LANG_STRINGS["info_systemd"]="systemd is beschikbaar"
            LANG_STRINGS["error_arch"]="Niet-ondersteunde architectuur: %s"
            LANG_STRINGS["error_binary_not_found"]="QuantMesh binair bestand niet gevonden. Zorg ervoor dat u dit script uitvoert in de uitgepakte map."
            LANG_STRINGS["error_binary_expected"]="Verwacht: %s of quantmesh"
            LANG_STRINGS["info_binary_found"]="Binair bestand gevonden: %s"
            LANG_STRINGS["warn_config_not_found"]="config.example.yaml niet gevonden, installatie van configuratiebestand wordt overgeslagen"
            LANG_STRINGS["info_config_found"]="Configuratievoorbeeld gevonden: %s"
            LANG_STRINGS["warn_service_not_found"]="quantmesh.service-sjabloon niet gevonden, ingebouwd sjabloon wordt gebruikt"
            LANG_STRINGS["info_service_found"]="Servicebestandsjabloon gevonden: %s"
            LANG_STRINGS["step_create_user"]="Gebruiker quantmesh aanmaken..."
            LANG_STRINGS["info_user_created"]="Gebruiker quantmesh aangemaakt"
            LANG_STRINGS["info_user_exists"]="Gebruiker quantmesh bestaat al"
            LANG_STRINGS["step_create_dirs"]="Installatiemappen aanmaken..."
            LANG_STRINGS["info_dirs_created"]="Mappen aangemaakt: %s"
            LANG_STRINGS["step_stop_service"]="Bestaande %s service stoppen..."
            LANG_STRINGS["info_service_stopped"]="Service gestopt"
            LANG_STRINGS["step_install_binary"]="Binair bestand installeren..."
            LANG_STRINGS["info_binary_installed"]="Binair bestand geïnstalleerd: %s (versie: %s)"
            LANG_STRINGS["step_handle_config"]="Configuratiebestand verwerken..."
            LANG_STRINGS["warn_config_exists"]="Bestaand configuratiebestand gedetecteerd: %s"
            LANG_STRINGS["config_choice_prompt"]="Selecteer hoe u wilt doorgaan:"
            LANG_STRINGS["config_choice_1"]="Bestaande configuratie behouden (standaard, aanbevolen)"
            LANG_STRINGS["config_choice_2"]="Overschrijven met nieuw configuratievoorbeeld (oude configuratie wordt opgeslagen)"
            LANG_STRINGS["config_choice_3"]="Configuratie samenvoegen (oude behouden, voorbeeld kopiëren als config.example.yaml)"
            LANG_STRINGS["config_select"]="Selecteer [1/2/3] (standaard: 1): "
            LANG_STRINGS["info_config_kept"]="Bestaande configuratie behouden"
            LANG_STRINGS["info_config_example_copied"]="Configuratievoorbeeld gekopieerd naar: %s"
            LANG_STRINGS["info_config_backed_up"]="Oude configuratie opgeslagen in: %s"
            LANG_STRINGS["warn_config_updated"]="Configuratiebestand bijgewerkt, bewerk %s om uw API-sleutels in te stellen, enz."
            LANG_STRINGS["info_config_merged"]="Bestaande configuratie behouden, voorbeeldconfiguratie gekopieerd ter referentie"
            LANG_STRINGS["info_invalid_choice"]="Ongeldige keuze, bestaande configuratie wordt behouden"
            LANG_STRINGS["warn_config_created"]="Configuratiebestand aangemaakt: %s"
            LANG_STRINGS["warn_config_edit"]="Bewerk het configuratiebestand en voer uw API-sleutels en handelsparameters in!"
            LANG_STRINGS["step_install_service"]="systemd-service installeren..."
            LANG_STRINGS["info_service_installed"]="systemd-service geïnstalleerd en ingeschakeld"
            LANG_STRINGS["step_set_permissions"]="Bestandsrechten instellen..."
            LANG_STRINGS["info_permissions_set"]="Rechten ingesteld"
            LANG_STRINGS["step_copy_scripts"]="Hulpscripts kopiëren..."
            LANG_STRINGS["info_scripts_copied"]="Hulpscripts gekopieerd"
            LANG_STRINGS["prompt_start_service"]="Wilt u de QuantMesh-service nu starten? [y/N] (standaard: N): "
            LANG_STRINGS["step_start_service"]="Service starten..."
            LANG_STRINGS["info_service_started"]="Service succesvol gestart"
            LANG_STRINGS["error_service_failed"]="Service starten mislukt, controleer de logs: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="Service niet gestart. U kunt deze later starten met het volgende commando:"
            LANG_STRINGS["success_title"]="QuantMesh installatie voltooid!"
            LANG_STRINGS["info_install_dir"]="Installatiemap: %s"
            LANG_STRINGS["info_config_file"]="Configuratiebestand: %s"
            LANG_STRINGS["info_data_dir"]="Gegevensmap: %s"
            LANG_STRINGS["info_backup_dir"]="Back-upmap: %s"
            LANG_STRINGS["info_logs_dir"]="Logmap: %s"
            LANG_STRINGS["common_commands"]="Veelgebruikte commando's:"
            LANG_STRINGS["cmd_start"]="Service starten:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="Service stoppen:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="Service herstarten:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="Status bekijken:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="Logs bekijken:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="Webinterface:    http://localhost:28888"
            LANG_STRINGS["important_note"]="Belangrijke opmerking:"
            LANG_STRINGS["note_edit_config"]="Bewerk eerst het configuratiebestand en voer uw exchange API-sleutels in:"
            LANG_STRINGS["step_start_install"]="Installatie starten..."
            ;;
        *)
            # English (default)
            LANG_STRINGS["title"]="QuantMesh Installation Script"
            LANG_STRINGS["error_root"]="Please run this script as root: sudo ./install.sh"
            LANG_STRINGS["info_os_detected"]="Detected operating system: %s %s"
            LANG_STRINGS["error_systemd"]="systemd is not available. This installation script only supports Linux systems using systemd."
            LANG_STRINGS["info_systemd"]="systemd is available"
            LANG_STRINGS["error_arch"]="Unsupported architecture: %s"
            LANG_STRINGS["error_binary_not_found"]="QuantMesh binary file not found. Please ensure you run this script in the extracted directory."
            LANG_STRINGS["error_binary_expected"]="Expected to find: %s or quantmesh"
            LANG_STRINGS["info_binary_found"]="Binary file found: %s"
            LANG_STRINGS["warn_config_not_found"]="config.example.yaml not found, will skip configuration file installation"
            LANG_STRINGS["info_config_found"]="Configuration example found: %s"
            LANG_STRINGS["warn_service_not_found"]="quantmesh.service template not found, will use built-in template"
            LANG_STRINGS["info_service_found"]="Service file template found: %s"
            LANG_STRINGS["step_create_user"]="Creating quantmesh user..."
            LANG_STRINGS["info_user_created"]="User quantmesh created"
            LANG_STRINGS["info_user_exists"]="User quantmesh already exists"
            LANG_STRINGS["step_create_dirs"]="Creating installation directories..."
            LANG_STRINGS["info_dirs_created"]="Directories created: %s"
            LANG_STRINGS["step_stop_service"]="Stopping existing %s service..."
            LANG_STRINGS["info_service_stopped"]="Service stopped"
            LANG_STRINGS["step_install_binary"]="Installing binary file..."
            LANG_STRINGS["info_binary_installed"]="Binary file installed: %s (version: %s)"
            LANG_STRINGS["step_handle_config"]="Handling configuration file..."
            LANG_STRINGS["warn_config_exists"]="Existing configuration file detected: %s"
            LANG_STRINGS["config_choice_prompt"]="Please select how to proceed:"
            LANG_STRINGS["config_choice_1"]="Keep existing configuration (default, recommended)"
            LANG_STRINGS["config_choice_2"]="Overwrite with new example configuration (old configuration will be backed up)"
            LANG_STRINGS["config_choice_3"]="Merge configuration (keep old configuration, copy example as config.example.yaml)"
            LANG_STRINGS["config_select"]="Select [1/2/3] (default: 1): "
            LANG_STRINGS["info_config_kept"]="Existing configuration kept"
            LANG_STRINGS["info_config_example_copied"]="Example configuration copied to: %s"
            LANG_STRINGS["info_config_backed_up"]="Old configuration backed up to: %s"
            LANG_STRINGS["warn_config_updated"]="Configuration file updated, please edit %s to configure your API keys, etc."
            LANG_STRINGS["info_config_merged"]="Existing configuration kept, example configuration copied for reference"
            LANG_STRINGS["info_invalid_choice"]="Invalid choice, keeping existing configuration"
            LANG_STRINGS["warn_config_created"]="Configuration file created: %s"
            LANG_STRINGS["warn_config_edit"]="Please edit the configuration file and enter your API keys and trading parameters!"
            LANG_STRINGS["step_install_service"]="Installing systemd service..."
            LANG_STRINGS["info_service_installed"]="systemd service installed and enabled"
            LANG_STRINGS["step_set_permissions"]="Setting file permissions..."
            LANG_STRINGS["info_permissions_set"]="Permissions set"
            LANG_STRINGS["step_copy_scripts"]="Copying auxiliary scripts..."
            LANG_STRINGS["info_scripts_copied"]="Auxiliary scripts copied"
            LANG_STRINGS["prompt_start_service"]="Do you want to start the QuantMesh service now? [y/N] (default: N): "
            LANG_STRINGS["step_start_service"]="Starting service..."
            LANG_STRINGS["info_service_started"]="Service started successfully"
            LANG_STRINGS["error_service_failed"]="Service failed to start, please check logs: journalctl -u %s -f"
            LANG_STRINGS["info_service_not_started"]="Service not started. You can start it later with the following command:"
            LANG_STRINGS["success_title"]="QuantMesh installation completed!"
            LANG_STRINGS["info_install_dir"]="Installation directory: %s"
            LANG_STRINGS["info_config_file"]="Configuration file: %s"
            LANG_STRINGS["info_data_dir"]="Data directory: %s"
            LANG_STRINGS["info_backup_dir"]="Backup directory: %s"
            LANG_STRINGS["info_logs_dir"]="Logs directory: %s"
            LANG_STRINGS["common_commands"]="Common commands:"
            LANG_STRINGS["cmd_start"]="Start service:   sudo systemctl start %s"
            LANG_STRINGS["cmd_stop"]="Stop service:   sudo systemctl stop %s"
            LANG_STRINGS["cmd_restart"]="Restart service:   sudo systemctl restart %s"
            LANG_STRINGS["cmd_status"]="Check status:   sudo systemctl status %s"
            LANG_STRINGS["cmd_logs"]="View logs:   journalctl -u %s -f"
            LANG_STRINGS["web_ui"]="Web interface:    http://localhost:28888"
            LANG_STRINGS["important_note"]="Important note:"
            LANG_STRINGS["note_edit_config"]="Please edit the configuration file first and enter your exchange API keys:"
            LANG_STRINGS["step_start_install"]="Starting installation..."
            ;;
    esac
}

# Translation function
t() {
    local key=$1
    shift
    local str="${LANG_STRINGS[$key]}"
    if [ -z "$str" ]; then
        echo "[MISSING: $key]"
        return
    fi
    # Use printf for proper formatting with %s placeholders
    printf "$str" "$@"
}

# ============================================================================
# Logging Functions
# ============================================================================

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_step() {
    echo -e "${BLUE}[STEP]${NC} $1"
}

# ============================================================================
# Installation Functions
# ============================================================================

# Check if running as root
check_root() {
    if [ "$EUID" -ne 0 ]; then
        log_error "$(t "error_root")"
        exit 1
    fi
}

# Detect operating system
detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        OS=$NAME
        OS_VERSION=$VERSION_ID
    else
        OS=$(uname -s)
        OS_VERSION=$(uname -r)
    fi
    log_info "$(t "info_os_detected" "$OS" "$OS_VERSION")"
}

# Check if systemd is available
check_systemd() {
    if ! command -v systemctl &> /dev/null; then
        log_error "$(t "error_systemd")"
        exit 1
    fi
    log_info "$(t "info_systemd")"
}

# Find binary file
find_binary() {
    local arch=$(uname -m)
    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    
    # Convert architecture name
    case $arch in
        x86_64)
            arch="amd64"
            ;;
        aarch64|arm64)
            arch="arm64"
            ;;
        *)
            log_error "$(t "error_arch" "$arch")"
            exit 1
            ;;
    esac
    
    # Find binary file
    local binary_name="quantmesh-${os}-${arch}"
    
    # First check current directory
    if [ -f "./${binary_name}" ]; then
        BINARY_PATH="./${binary_name}"
    elif [ -f "${SCRIPT_DIR}/${binary_name}" ]; then
        BINARY_PATH="${SCRIPT_DIR}/${binary_name}"
    elif [ -f "${SCRIPT_DIR}/../${binary_name}" ]; then
        BINARY_PATH="${SCRIPT_DIR}/../${binary_name}"
    elif [ -f "./quantmesh" ]; then
        BINARY_PATH="./quantmesh"
    elif [ -f "${SCRIPT_DIR}/quantmesh" ]; then
        BINARY_PATH="${SCRIPT_DIR}/quantmesh"
    elif [ -f "${SCRIPT_DIR}/../quantmesh" ]; then
        BINARY_PATH="${SCRIPT_DIR}/../quantmesh"
    else
        log_error "$(t "error_binary_not_found")"
        log_error "$(t "error_binary_expected" "$binary_name")"
        exit 1
    fi
    
    log_info "$(t "info_binary_found" "$BINARY_PATH")"
}

# Find configuration example file
find_config_example() {
    if [ -f "./config.example.yaml" ]; then
        CONFIG_EXAMPLE="./config.example.yaml"
    elif [ -f "${SCRIPT_DIR}/config.example.yaml" ]; then
        CONFIG_EXAMPLE="${SCRIPT_DIR}/config.example.yaml"
    elif [ -f "${SCRIPT_DIR}/../config.example.yaml" ]; then
        CONFIG_EXAMPLE="${SCRIPT_DIR}/../config.example.yaml"
    else
        log_warn "$(t "warn_config_not_found")"
        CONFIG_EXAMPLE=""
    fi
    
    if [ -n "$CONFIG_EXAMPLE" ]; then
        log_info "$(t "info_config_found" "$CONFIG_EXAMPLE")"
    fi
}

# Find systemd service file
find_service_file() {
    if [ -f "./quantmesh.service" ]; then
        SERVICE_TEMPLATE="./quantmesh.service"
    elif [ -f "${SCRIPT_DIR}/quantmesh.service" ]; then
        SERVICE_TEMPLATE="${SCRIPT_DIR}/quantmesh.service"
    elif [ -f "${SCRIPT_DIR}/../quantmesh.service" ]; then
        SERVICE_TEMPLATE="${SCRIPT_DIR}/../quantmesh.service"
    elif [ -f "${SCRIPT_DIR}/../scripts/quantmesh.service" ]; then
        SERVICE_TEMPLATE="${SCRIPT_DIR}/../scripts/quantmesh.service"
    else
        log_warn "$(t "warn_service_not_found")"
        SERVICE_TEMPLATE=""
    fi
    
    if [ -n "$SERVICE_TEMPLATE" ]; then
        log_info "$(t "info_service_found" "$SERVICE_TEMPLATE")"
    fi
}

# Create quantmesh user and group
create_user() {
    if ! id -u quantmesh &>/dev/null; then
        log_step "$(t "step_create_user")"
        useradd -r -s /bin/false -d ${INSTALL_DIR} quantmesh
        log_info "$(t "info_user_created")"
    else
        log_info "$(t "info_user_exists")"
    fi
}

# Create necessary directories
create_directories() {
    log_step "$(t "step_create_dirs")"
    
    mkdir -p ${INSTALL_DIR}
    mkdir -p ${BACKUP_DIR}
    mkdir -p ${DATA_DIR}
    mkdir -p ${LOGS_DIR}
    mkdir -p ${INSTALL_DIR}/scripts
    mkdir -p ${INSTALL_DIR}/backtest/results
    mkdir -p ${INSTALL_DIR}/backtest/reports
    mkdir -p ${INSTALL_DIR}/backtest/cache
    mkdir -p ${INSTALL_DIR}/backtest/optim_results
    
    log_info "$(t "info_dirs_created" "$INSTALL_DIR")"
}

# Stop existing service
stop_existing_service() {
    if systemctl is-active --quiet ${SERVICE_NAME} 2>/dev/null; then
        log_step "$(t "step_stop_service" "$SERVICE_NAME")"
        systemctl stop ${SERVICE_NAME}
        log_info "$(t "info_service_stopped")"
    fi
}

# Install binary file
install_binary() {
    log_step "$(t "step_install_binary")"
    
    cp "${BINARY_PATH}" "${INSTALL_DIR}/quantmesh"
    chmod +x "${INSTALL_DIR}/quantmesh"
    
    # Get version information
    local version=$("${INSTALL_DIR}/quantmesh" --version 2>/dev/null || echo "unknown")
    log_info "$(t "info_binary_installed" "${INSTALL_DIR}/quantmesh" "$version")"
    
    # Save version info for telemetry
    INSTALLED_VERSION=$version
}

# Handle configuration file
handle_config() {
    if [ -z "$CONFIG_EXAMPLE" ]; then
        return
    fi
    
    log_step "$(t "step_handle_config")"
    
    if [ -f "$CONFIG_FILE" ]; then
        echo ""
        echo -e "${YELLOW}$(t "warn_config_exists" "$CONFIG_FILE")${NC}"
        echo ""
        echo "$(t "config_choice_prompt")"
        echo "  [1] $(t "config_choice_1")"
        echo "  [2] $(t "config_choice_2")"
        echo "  [3] $(t "config_choice_3")"
        echo ""
        
        read -p "$(t "config_select")" config_choice
        config_choice=${config_choice:-1}
        
        case $config_choice in
            1)
                log_info "$(t "info_config_kept")"
                cp "${CONFIG_EXAMPLE}" "${INSTALL_DIR}/config.example.yaml"
                log_info "$(t "info_config_example_copied" "${INSTALL_DIR}/config.example.yaml")"
                ;;
            2)
                local backup_name="config.yaml.backup.$(date +%Y%m%d_%H%M%S)"
                local backup_path="${BACKUP_DIR}/${backup_name}"
                cp "$CONFIG_FILE" "$backup_path"
                log_info "$(t "info_config_backed_up" "$backup_path")"
                
                cp "${CONFIG_EXAMPLE}" "$CONFIG_FILE"
                log_warn "$(t "warn_config_updated" "$CONFIG_FILE")"
                ;;
            3)
                log_info "$(t "info_config_merged")"
                cp "${CONFIG_EXAMPLE}" "${INSTALL_DIR}/config.example.yaml"
                log_info "$(t "info_config_example_copied" "${INSTALL_DIR}/config.example.yaml")"
                ;;
            *)
                log_info "$(t "info_invalid_choice")"
                ;;
        esac
    else
        cp "${CONFIG_EXAMPLE}" "$CONFIG_FILE"
        cp "${CONFIG_EXAMPLE}" "${INSTALL_DIR}/config.example.yaml"
        log_warn "$(t "warn_config_created" "$CONFIG_FILE")"
        log_warn "$(t "warn_config_edit")"
    fi
}

# Generate built-in systemd service file
generate_service_file() {
    cat > "${SERVICE_FILE}" << 'EOF'
[Unit]
Description=QuantMesh Market Maker Service
Documentation=https://quantmesh.io
After=network.target

[Service]
Type=simple
User=quantmesh
Group=quantmesh
WorkingDirectory=/opt/quantmesh
ExecStart=/opt/quantmesh/quantmesh
ExecStop=/bin/kill -s TERM $MAINPID

# Restart policy
Restart=on-failure
RestartSec=10s
StartLimitInterval=5min
StartLimitBurst=3

# Resource limits
LimitNOFILE=65536
LimitNPROC=4096

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/quantmesh/data /opt/quantmesh/logs /opt/quantmesh/backups /opt/quantmesh/config.yaml /opt/quantmesh/config_backups /opt/quantmesh/backtest

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=quantmesh

[Install]
WantedBy=multi-user.target
EOF
}

# Install systemd service
install_service() {
    log_step "$(t "step_install_service")"
    
    if [ -n "$SERVICE_TEMPLATE" ]; then
        cp "${SERVICE_TEMPLATE}" "${SERVICE_FILE}"
    else
        generate_service_file
    fi
    
    # Reload systemd
    systemctl daemon-reload
    
    # Enable service
    systemctl enable ${SERVICE_NAME}
    
    log_info "$(t "info_service_installed")"
}

# Set permissions
set_permissions() {
    log_step "$(t "step_set_permissions")"
    
    chown -R quantmesh:quantmesh ${INSTALL_DIR}
    chmod 755 ${INSTALL_DIR}
    chmod 700 ${DATA_DIR}
    chmod 0776 ${BACKUP_DIR}
    chmod 700 ${LOGS_DIR}
    chmod -R 775 ${INSTALL_DIR}/backtest
    
    # Configuration file permissions (contains sensitive information)
    if [ -f "$CONFIG_FILE" ]; then
        chmod 600 "$CONFIG_FILE"
    fi
    
    log_info "$(t "info_permissions_set")"
}

# Copy auxiliary scripts
copy_scripts() {
    log_step "$(t "step_copy_scripts")"
    
    # Copy backup.sh and restore.sh (if they exist)
    for script in backup.sh restore.sh; do
        if [ -f "${SCRIPT_DIR}/${script}" ]; then
            cp "${SCRIPT_DIR}/${script}" "${INSTALL_DIR}/scripts/"
            chmod +x "${INSTALL_DIR}/scripts/${script}"
        elif [ -f "${SCRIPT_DIR}/../scripts/${script}" ]; then
            cp "${SCRIPT_DIR}/../scripts/${script}" "${INSTALL_DIR}/scripts/"
            chmod +x "${INSTALL_DIR}/scripts/${script}"
        fi
    done
    
    log_info "$(t "info_scripts_copied")"
}

# Start service
start_service() {
    echo ""
    read -p "$(t "prompt_start_service")" start_now
    start_now=${start_now:-N}
    
    if [[ "$start_now" =~ ^[Yy]$ ]]; then
        log_step "$(t "step_start_service")"
        systemctl start ${SERVICE_NAME}
        
        # Wait a few seconds and check status
        sleep 3
        
        if systemctl is-active --quiet ${SERVICE_NAME}; then
            log_info "$(t "info_service_started")"
        else
            log_error "$(t "error_service_failed" "$SERVICE_NAME")"
        fi
    else
        log_info "$(t "info_service_not_started")"
        echo "  sudo systemctl start ${SERVICE_NAME}"
    fi
}

# Send installation telemetry (optional, fully transparent)
send_install_telemetry() {
    # Check if telemetry is disabled
    if [ "$QUANTMESH_DISABLE_TELEMETRY" = "1" ]; then
        return
    fi
    
    # Check if curl command exists
    if ! command -v curl &> /dev/null; then
        return
    fi
    
    # PostHog Project ID
    POSTHOG_PROJECT_ID="${QUANTMESH_TELEMETRY_PROJECT_ID:-phc_kz2U334i5MD8ozz78zvCdN6aRkkx3kYyoU1RSigJOiA}"
    POSTHOG_ENDPOINT="${QUANTMESH_TELEMETRY_ENDPOINT:-https://us.i.posthog.com/capture/}"
    
    # Skip if not configured
    if [ -z "$POSTHOG_PROJECT_ID" ] || [ "$POSTHOG_PROJECT_ID" = "YOUR_POSTHOG_PROJECT_ID" ]; then
        return
    fi
    
    # Get system information
    local arch=$(uname -m)
    case $arch in
        x86_64) arch="amd64" ;;
        aarch64|arm64) arch="arm64" ;;
        *) arch="unknown" ;;
    esac
    
    local os_name=$(uname -s | tr '[:upper:]' '[:lower:]')
    local timestamp=$(date -Iseconds 2>/dev/null || date +"%Y-%m-%dT%H:%M:%S%z")
    
    # Send telemetry asynchronously, don't block installation
    (
        hname=$(hostname 2>/dev/null || echo "unknown")
        public_ip=$(curl -s -m 1.5 "https://ip4.dev/myip" 2>/dev/null | tr -d '[:space:]')
        distinct_id="${hname}"
        [ -n "$public_ip" ] && distinct_id="${hname}-${public_ip}"
        distinct_id="${distinct_id}-${os_name}-${arch}-${INSTALLED_VERSION}"
        curl -s -m 1.5 -X POST "${POSTHOG_ENDPOINT}" \
            -H "Content-Type: application/json" \
            -H "User-Agent: QuantMesh-InstallScript/${INSTALLED_VERSION}" \
            -d "{
                \"api_key\": \"${POSTHOG_PROJECT_ID}\",
                \"event\": \"install\",
                \"distinct_id\": \"${distinct_id}\",
                \"properties\": {
                    \"timestamp\": \"${timestamp}\",
                    \"version\": \"${INSTALLED_VERSION}\",
                    \"os\": \"${os_name}\",
                    \"arch\": \"${arch}\"
                }
            }" > /dev/null 2>&1
    ) &
}

# Print completion information
print_completion() {
    echo ""
    echo "=============================================="
    echo -e "${GREEN}$(t "success_title")${NC}"
    echo "=============================================="
    echo ""
    echo "$(t "info_install_dir" "$INSTALL_DIR")"
    echo "$(t "info_config_file" "$CONFIG_FILE")"
    echo "$(t "info_data_dir" "$DATA_DIR")"
    echo "$(t "info_backup_dir" "$BACKUP_DIR")"
    echo "$(t "info_logs_dir" "$LOGS_DIR")"
    echo ""
    echo "$(t "common_commands")"
    echo "  $(t "cmd_start" "$SERVICE_NAME")"
    echo "  $(t "cmd_stop" "$SERVICE_NAME")"
    echo "  $(t "cmd_restart" "$SERVICE_NAME")"
    echo "  $(t "cmd_status" "$SERVICE_NAME")"
    echo "  $(t "cmd_logs" "$SERVICE_NAME")"
    echo ""
    echo "$(t "web_ui")"
    echo ""
    
    if [ ! -f "$CONFIG_FILE" ] || grep -q "YOUR_API_KEY" "$CONFIG_FILE" 2>/dev/null; then
        echo -e "${YELLOW}$(t "important_note")${NC}"
        echo "  $(t "note_edit_config")"
        echo "  sudo nano ${CONFIG_FILE}"
        echo ""
    fi
    
    # Send installation telemetry (asynchronous, non-blocking)
    send_install_telemetry
}

# Main function
main() {
    # Select language first (before any other output)
    select_language
    
    check_root
    detect_os
    check_systemd
    find_binary
    find_config_example
    find_service_file
    
    echo ""
    log_step "$(t "step_start_install")"
    echo ""
    
    create_user
    create_directories
    stop_existing_service
    install_binary
    handle_config
    install_service
    copy_scripts
    set_permissions
    start_service
    print_completion
}

# Run main function
main "$@"
