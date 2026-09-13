<div>
    <div class="bg-paper border-b border-stone-200">
        <div class="max-w-[1600px] mx-auto py-6 px-4 sm:px-6 lg:px-8">
            <h2 class="font-semibold text-xl text-stone-800">Settings</h2>
            <p class="text-sm text-stone-500 mt-1">Configure outgoing mail for invites, login codes, and password resets.</p>
        </div>
    </div>

    <div class="max-w-2xl mx-auto py-8 px-4 sm:px-6 lg:px-8">
        <div class="rounded-xl bg-paper shadow-soft border border-stone-200 p-6">
            <h3 class="font-medium text-stone-800 mb-1">SMTP</h3>
            <p class="text-sm text-stone-500 mb-4">Without this configured, mail falls back to being written to the server log only — nobody actually receives it.</p>

            @if (session('status'))
                <div class="mb-4 text-sm font-medium text-green-600">{{ session('status') }}</div>
            @endif

            <form wire:submit="save" class="space-y-4">
                <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    <div class="sm:col-span-2">
                        <x-input-label for="smtpHost" value="Host" />
                        <x-text-input id="smtpHost" wire:model="smtpHost" class="mt-1 block w-full" placeholder="smtp.example.com" />
                        <x-input-error :messages="$errors->get('smtpHost')" class="mt-1" />
                    </div>

                    <div>
                        <x-input-label for="smtpPort" value="Port" />
                        <x-text-input id="smtpPort" type="number" wire:model="smtpPort" class="mt-1 block w-full" />
                        <x-input-error :messages="$errors->get('smtpPort')" class="mt-1" />
                    </div>

                    <div>
                        <x-input-label for="smtpEncryption" value="Encryption" />
                        <select id="smtpEncryption" wire:model="smtpEncryption" class="mt-1 block w-full border-stone-300 rounded-md shadow-sm focus:border-tan-400 focus:ring-tan-400">
                            <option value="tls">TLS (STARTTLS, usually port 587)</option>
                            <option value="ssl">SSL (usually port 465)</option>
                            <option value="none">None</option>
                        </select>
                    </div>

                    <div>
                        <x-input-label for="smtpUsername" value="Username" />
                        <x-text-input id="smtpUsername" wire:model="smtpUsername" class="mt-1 block w-full" />
                        <x-input-error :messages="$errors->get('smtpUsername')" class="mt-1" />
                    </div>

                    <div>
                        <x-input-label for="smtpPassword" value="Password" />
                        <x-text-input id="smtpPassword" type="password" wire:model="smtpPassword" class="mt-1 block w-full" placeholder="Leave blank to keep current" />
                        <x-input-error :messages="$errors->get('smtpPassword')" class="mt-1" />
                    </div>

                    <div>
                        <x-input-label for="smtpFromAddress" value="From address" />
                        <x-text-input id="smtpFromAddress" type="email" wire:model="smtpFromAddress" class="mt-1 block w-full" placeholder="hello@example.com" />
                        <x-input-error :messages="$errors->get('smtpFromAddress')" class="mt-1" />
                    </div>

                    <div>
                        <x-input-label for="smtpFromName" value="From name" />
                        <x-text-input id="smtpFromName" wire:model="smtpFromName" class="mt-1 block w-full" placeholder="{{ config('app.name') }}" />
                        <x-input-error :messages="$errors->get('smtpFromName')" class="mt-1" />
                    </div>
                </div>

                <div class="flex items-center justify-between pt-2">
                    <button type="button" wire:click="sendTestEmail" class="text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-stone-600 hover:bg-stone-50 transition">
                        Send test email to myself
                    </button>
                    <x-primary-button>Save</x-primary-button>
                </div>

                @if ($testEmailStatus)
                    <p class="text-xs text-stone-500">{{ $testEmailStatus }}</p>
                @endif
            </form>
        </div>
    </div>
</div>
