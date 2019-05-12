(function() {
    function PV() {
        this.names = [];
    }

    PV.prototype.onObjectSelected = function(obj) {
        if (sprintf("%T", obj).split(".")[1] == "PersistentVolumeClaim") {
            this.names = [obj.Spec.VolumeName];

            if (!this.names.length) {
                return null
            }

            return {"Label": "Persistent Volume", "Callback": this.actionCallback.bind(this)}
        }

        return null
    }

    PV.prototype.actionCallback = function() {
        var name = this.names[0];

        if (this.names.length > 1) {
            // Displays a list dialog with the string array, returning the
            // user-selected one
            name = kd.PickFrom("Persistent Volumes", this.names)
        }

        // Client holds the k8s.Client object
        var secret = kd.Client().CoreV1().PersistentVolumes().Get(name, {})

        // Display can show plain text, or a k8s object
        kd.Display(secret)
    }

    PV.prototype.update = function(c, obj) {
        c.CoreV1().PersistentVolumes().Update(obj)
    }

    PV.prototype.del = function(c, obj, opts) {
        c.CoreV1().PersistentVolumes().Delete(obj.Name, opts)
    }

    PV.prototype.summary = function(obj) {
        return sprintf(
            "[skyblue::b]Status:[white::-] %s\n" +
            "[skyblue::b]Access Modes:[white::-] %s\n" +
            "[skyblue::b]Reclaim Policy:[white::-] %s\n" +
            "[skyblue::b]Claim:[white::-] %s\n" +
            "[skyblue::b]Storage Class:[white::-] %s\n",
            obj.Status.Phase,
            obj.Spec.AccessModes.join(", "),
            obj.Spec.PersistentVolumeReclaimPolicy,
            obj.Spec.ClaimRef ? obj.Spec.ClaimRef.Name : "",
            obj.Spec.StorageClassName
        )
    }

    var pv = new PV()

    // Callback for when an object is selected in the tree
    // Return null if no action is to be taken.
    // Return ["Action name", callback] otherwise.
    kd.RegisterActionOnObjectSelected(pv.onObjectSelected.bind(pv))

    // Register callbacks that deal with object operation of a certain type
    kd.RegisterControllerOperator("PersistentVolume", {
        "Update": pv.update.bind(pv),
        "Delete": pv.del.bind(pv)
    })

    // Register callbacks that deal with object mutation of a certain type
    kd.RegisterObjectSummaryProvider("PersistentVolume", pv.summary.bind(pv))
})()
