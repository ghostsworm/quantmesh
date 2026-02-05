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
#   - Multi-language support (Simplified Chinese, Traditional Chinese, English, Spanish, French)
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
    echo "Please select your language / 请选择您的语言 / Seleccione su idioma / Choisissez votre langue:"
    echo ""
    echo "  [1] English"
    echo "  [2] 简体中文 (Simplified Chinese)"
    echo "  [3] 繁體中文 (Traditional Chinese)"
    echo "  [4] Español (Spanish)"
    echo "  [5] Français (French)"
    echo ""
    
    while true; do
        read -p "Select language [1-5] (default: 1): " lang_choice
        lang_choice=${lang_choice:-1}
        
        case $lang_choice in
            1) LANG_CODE="en"; break ;;
            2) LANG_CODE="zh-CN"; break ;;
            3) LANG_CODE="zh-TW"; break ;;
            4) LANG_CODE="es"; break ;;
            5) LANG_CODE="fr"; break ;;
            *)
                echo "Invalid choice. Please enter 1-5."
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
