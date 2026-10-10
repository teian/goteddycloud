// SPDX-License-Identifier: GPL-2.0-only
import { useTranslation } from "react-i18next";
const labels = {
    users: ["Users and areas", "Benutzer und Bereiche"],
    shares: ["Shared catalogs", "Geteilte Kataloge"],
    area: ["Area", "Bereich"],
    global: ["Global administration", "Globale Administration"],
    admin: ["Area administrator", "Bereichsadministrator"],
    editor: ["Editor", "Bearbeiter"],
    reader: ["Reader", "Leser"],
    accountAdmin: ["Server administrator", "Serveradministrator"],
    accountEditor: ["User (default: editor)", "Benutzer (Vorgabe: Bearbeiter)"],
    accountReader: ["User (default: reader)", "Benutzer (Vorgabe: Leser)"],
    name: ["Name", "Name"],
    username: ["Username", "Benutzername"],
    password: ["Password", "Passwort"],
    currentPassword: ["Current password", "Aktuelles Passwort"],
    newPassword: ["New password", "Neues Passwort"],
    save: ["Save", "Speichern"],
    create: ["Create", "Anlegen"],
    remove: ["Remove", "Entfernen"],
    edit: ["Edit", "Bearbeiten"],
    members: ["Members and roles", "Mitglieder und Rollen"],
    boxes: ["Assigned boxes", "Zugewiesene Boxen"],
    addMember: ["Add existing user", "Bestehenden Benutzer hinzufügen"],
    role: ["Role", "Rolle"],
    createArea: ["Create shared area", "Gemeinsamen Bereich anlegen"],
    accounts: ["Accounts", "Konten"],
    createUser: ["Create account", "Konto anlegen"],
    ownPassword: ["Change own password", "Eigenes Passwort ändern"],
    resetPassword: ["Reset account password", "Kontopasswort zurücksetzen"],
    login: ["Sign in", "Anmelden"],
    loginNeeded: [
        "Sign in to view and share catalogs.",
        "Anmelden, um Kataloge anzusehen und zu teilen.",
    ],
    enabled: ["Require web login", "Web-Login erzwingen"],
    override: [
        "Login protection is bypassed by the recovery environment setting.",
        "Die Wiederherstellungseinstellung umgeht den Login-Schutz.",
    ],
    refresh: ["Refresh", "Aktualisieren"],
    download: ["Download", "Herunterladen"],
    catalog: ["Catalog", "Katalog"],
    recipient: ["Recipient (existing username)", "Empfänger (bestehender Benutzername)"],
    all: [
        "Entire catalog, including future content",
        "Gesamter Katalog, einschließlich zukünftiger Inhalte",
    ],
    selected: ["Selected files", "Ausgewählte Dateien"],
    outgoing: ["Outgoing grants", "Erteilte Freigaben"],
    grant: ["Save or replace grant", "Freigabe speichern oder ersetzen"],
    revoke: ["Revoke", "Widerrufen"],
    readOnly: [
        "Recipients can read and download stored audio. Grants do not give membership, write access or device access. Remote sources and credentials remain private.",
        "Empfänger können gespeicherte Audiodateien lesen und herunterladen. Freigaben erteilen keine Mitgliedschaft, Schreib- oder Boxrechte. Externe Quellen und Zugangsdaten bleiben privat.",
    ],
    selectFiles: [
        "Select at least one file or the entire catalog.",
        "Mindestens eine Datei oder den gesamten Katalog auswählen.",
    ],
    areasHelp: [
        "Area roles are independent. Area administrators manage existing members; server administrators assign boxes and manage accounts. Changing the selected area reloads the interface for all current views.",
        "Bereichsrollen sind unabhängig. Bereichsadministratoren verwalten bestehende Mitglieder; Serveradministratoren weisen Boxen zu und verwalten Konten. Ein Bereichswechsel lädt die Oberfläche neu.",
    ],
    accountHelp: [
        "Account roles set the default-area role; membership roles in other areas stay independent.",
        "Kontorollen bestimmen die Rolle im Standardbereich; Rollen in anderen Bereichen bleiben unabhängig.",
    ],
    removeArea: [
        "Remove this area? Files remain available for administrator recovery.",
        "Bereich entfernen? Dateien bleiben zur Wiederherstellung gespeichert.",
    ],
    removeUser: [
        "Delete this account? Memberships and received grants are removed.",
        "Konto löschen? Mitgliedschaften und empfangene Freigaben werden entfernt.",
    ],
    saved: ["Saved.", "Gespeichert."],
    empty: ["No stored audio available.", "Keine gespeicherten Audiodateien verfügbar."],
    manageArea: ["Manage area", "Bereich verwalten"],
    setup: [
        "Create the first administrator account, then enable login protection.",
        "Zuerst das Administratorkonto anlegen, danach den Login-Schutz aktivieren.",
    ],
} as const;
export type Label = keyof typeof labels;
export function useGoText() {
    const { i18n } = useTranslation();
    return (key: Label): string => labels[key][i18n.language.startsWith("de") ? 1 : 0];
}
