<?php

use App\Models\Machine;
use App\Models\User;
use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    /**
     * Run the migrations.
     *
     * Tracks a cross-machine drag/drop relay (hub downloads from the
     * source agent, uploads to the destination agent — see
     * App\Jobs\RelayTransferJob) so it can run on the queue instead of
     * blocking the request, with a status the Transfers panel can poll.
     * Same-machine transfers don't need this — they use the target
     * agent's own transfer queue (see docs/AGENT_API.md).
     */
    public function up(): void
    {
        Schema::create('relay_transfers', function (Blueprint $table) {
            $table->id();
            $table->foreignIdFor(Machine::class, 'source_machine_id')->constrained('machines')->cascadeOnDelete();
            $table->foreignIdFor(Machine::class, 'destination_machine_id')->constrained('machines')->cascadeOnDelete();
            $table->string('source_path');
            $table->string('destination_path');
            $table->enum('kind', ['copy', 'move'])->default('copy');
            // No byte-level progress here — a cross-machine relay is a
            // download-then-upload through the hub, not a single
            // chunked stream, so progress is tracked by phase instead.
            $table->enum('status', ['queued', 'downloading', 'uploading', 'done', 'error'])->default('queued');
            $table->text('error')->nullable();
            $table->foreignIdFor(User::class)->nullable()->constrained()->nullOnDelete();
            $table->timestamps();
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::dropIfExists('relay_transfers');
    }
};
