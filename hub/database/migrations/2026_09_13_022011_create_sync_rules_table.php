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
     */
    public function up(): void
    {
        Schema::create('sync_rules', function (Blueprint $table) {
            $table->id();
            $table->string('name');
            $table->foreignIdFor(Machine::class, 'source_machine_id')->constrained('machines')->cascadeOnDelete();
            $table->string('source_path');
            $table->foreignIdFor(Machine::class, 'destination_machine_id')->constrained('machines')->cascadeOnDelete();
            $table->string('destination_path');
            // one_way: copy new/changed source files to the destination,
            // never deletes there. mirror: also deletes destination files
            // that no longer exist in the source.
            $table->enum('direction', ['one_way', 'mirror'])->default('one_way');
            $table->unsignedInteger('interval_minutes')->default(60);
            $table->boolean('enabled')->default(true);
            $table->timestamp('last_run_at')->nullable();
            // Set when a destination file changed since it was last
            // scanned, so a sync run skipped it rather than clobbering it —
            // surfaced in the UI instead of failing the whole rule.
            $table->boolean('has_conflict')->default(false);
            $table->text('last_error')->nullable();
            $table->foreignIdFor(User::class, 'created_by')->nullable()->constrained('users')->nullOnDelete();
            $table->timestamps();
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::dropIfExists('sync_rules');
    }
};
